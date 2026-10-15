package request

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mailru/easyjson/jlexer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"example.com/companies/internal/platform/httpserver/response"
)

type probe struct {
	N Optional[int]
	S *string
}

func (p *probe) UnmarshalEasyJSON(in *jlexer.Lexer) {
	DecodeObject(in, func(name string) {
		switch name {
		case "n":
			p.N = ReadOptional(in, name, ReadInt)
		case "s":
			p.S = ReadPtr(in, name, ReadString)
		default:
			RejectField(in, name, "is not allowed")
		}
	})
}

func decode(t *testing.T, contentType, body string, maxBody int64) (probe, *response.Error) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	var p probe
	err := Decode(httptest.NewRecorder(), req, &p, maxBody, "application/json")
	if err == nil {
		return p, nil
	}
	var re *response.Error
	require.ErrorAs(t, err, &re)
	return p, re
}

func TestDecodeSuccess(t *testing.T) {
	p, err := decode(t, "application/json; charset=utf-8", `{"n": 7, "s": "x"}`, 1024)
	require.Nil(t, err)
	assert.Equal(t, Optional[int]{Value: 7, Set: true}, p.N)
	assert.Equal(t, "x", *p.S)

	p, err = decode(t, "application/json", `{"n": null}`, 1024)
	require.Nil(t, err)
	assert.Equal(t, Optional[int]{Set: true, Null: true}, p.N, "explicit null is not absence")
	assert.Nil(t, p.S)
}

func TestDecodeFailures(t *testing.T) {
	tests := []struct {
		name, contentType, body string
		status                  int
		code, message           string
		fields                  map[string]string
	}{
		{
			"content type",
			"text/plain",
			`{}`,
			415,
			response.CodeUnsupportedMedia,
			"Content-Type must be application/json",
			nil,
		},
		{
			"missing content type",
			"",
			`{}`,
			415,
			response.CodeUnsupportedMedia,
			"Content-Type must be application/json",
			nil,
		},
		{"empty", "application/json", "  \n", 400, response.CodeBadRequest, "request body must not be empty", nil},
		{
			"not an object",
			"application/json",
			`[1]`,
			400,
			response.CodeBadRequest,
			"request body must be a JSON object",
			nil,
		},
		{
			"truncated",
			"application/json",
			`{"n":`,
			400,
			response.CodeBadRequest,
			"request body contains malformed JSON",
			nil,
		},
		{
			"trailing data",
			"application/json",
			`{} {}`,
			400,
			response.CodeBadRequest,
			"request body contains malformed JSON",
			nil,
		},
		{
			"bare word",
			"application/json",
			`{n:1}`,
			400,
			response.CodeBadRequest,
			"request body contains malformed JSON",
			nil,
		},
		{
			"too large",
			"application/json",
			`{"s":"` + strings.Repeat("x", 100) + `"}`,
			413,
			response.CodeTooLarge,
			"request body must not exceed 32 bytes",
			nil,
		},
		{
			"wrong type",
			"application/json",
			`{"n":"7"}`,
			400,
			response.CodeValidation,
			"request validation failed",
			map[string]string{"n": "must be a JSON integer"},
		},
		{
			"fraction",
			"application/json",
			`{"n":1.5}`,
			400,
			response.CodeValidation,
			"request validation failed",
			map[string]string{"n": "must be a JSON integer"},
		},
		{
			"unknown member",
			"application/json",
			`{"x":1}`,
			400,
			response.CodeValidation,
			"request validation failed",
			map[string]string{"x": "is not allowed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decode(t, tt.contentType, tt.body, 32)
			require.NotNil(t, err)
			assert.Equal(t, tt.status, err.Status)
			assert.Equal(t, tt.code, err.Code)
			assert.Equal(t, tt.message, err.Message)
			assert.Equal(t, tt.fields, err.Fields)
		})
	}
}

func TestPathUUID(t *testing.T) {
	id := uuid.New()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	req.SetPathValue("id", id.String())
	got, err := PathUUID(req, "id")
	require.NoError(t, err)
	assert.Equal(t, id, got)

	req.SetPathValue("id", "not-a-uuid")
	_, err = PathUUID(req, "id")
	var re *response.Error
	require.ErrorAs(t, err, &re)
	assert.Equal(t, map[string]string{"id": "must be a valid UUID"}, re.Fields)
}
