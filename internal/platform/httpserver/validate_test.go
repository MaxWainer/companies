package httpserver

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mailru/easyjson/jlexer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"example.com/companies/internal/platform/httpserver/middleware"
	"example.com/companies/internal/platform/httpserver/request"
	"example.com/companies/internal/platform/httpserver/requestctx"
	"example.com/companies/internal/platform/httpserver/response"
)

type fieldErr map[string]string

func (e fieldErr) Error() string                  { return "invalid" }
func (e fieldErr) FieldErrors() map[string]string { return e }

type nameBody struct{ Name string }

func (b *nameBody) UnmarshalEasyJSON(in *jlexer.Lexer) {
	request.DecodeObject(in, func(name string) {
		if name != "name" {
			request.RejectField(in, name, "is not allowed")
			return
		}
		b.Name = request.ReadString(in, name)
	})
}

func (b *nameBody) Validate() error {
	if b.Name == "" {
		return fieldErr{"name": "is required"}
	}
	return nil
}

type seen struct {
	calls int
	id    string
	name  string
}

func validatedServer(t *testing.T, got *seen) http.Handler {
	t.Helper()
	return NewHandler(Options{
		Verifier:     stubVerifier{},
		MaxBodyBytes: 64,
		Routes: []Route{{
			Method: http.MethodPut, Path: "/things/{id}",
			Input: &middleware.Input{
				Body:       func() middleware.Body { return &nameBody{} },
				MediaTypes: []string{"application/json"},
				PathUUIDs:  []string{"id"},
			},
			Handler: func(w http.ResponseWriter, r *http.Request) {
				got.calls++
				got.id = requestctx.ID(r.Context(), "id").String()
				got.name = requestctx.Body[*nameBody](r.Context()).Name
				w.WriteHeader(http.StatusOK)
			},
		}},
	})
}

func TestValidateRejectsBadInputBeforeTheHandler(t *testing.T) {
	id := uuid.NewString()
	tests := map[string]struct {
		path, contentType, body, token string
		status                         int
		code                           string
		fields                         map[string]string
	}{
		"auth runs first": {"/things/x", "text/plain", "", "", http.StatusUnauthorized, response.CodeUnauthorized, nil},
		"bad id": {
			"/things/x",
			"application/json",
			`{"name":"a"}`,
			validToken,
			http.StatusBadRequest,
			response.CodeValidation,
			map[string]string{"id": "must be a valid UUID"},
		},
		"media type": {
			"/things/" + id,
			"text/plain",
			`{"name":"a"}`,
			validToken,
			http.StatusUnsupportedMediaType,
			response.CodeUnsupportedMedia,
			nil,
		},
		"malformed": {
			"/things/" + id,
			"application/json",
			`{"name":`,
			validToken,
			http.StatusBadRequest,
			response.CodeBadRequest,
			nil,
		},
		"too large": {
			"/things/" + id,
			"application/json",
			`{"name":"` + strings.Repeat("x", 100) + `"}`,
			validToken,
			http.StatusRequestEntityTooLarge,
			response.CodeTooLarge,
			nil,
		},
		"unknown field": {
			"/things/" + id,
			"application/json",
			`{"nam":"a"}`,
			validToken,
			http.StatusBadRequest,
			response.CodeValidation,
			map[string]string{"nam": "is not allowed"},
		},
		"body Validate": {
			"/things/" + id,
			"application/json",
			`{}`,
			validToken,
			http.StatusBadRequest,
			response.CodeValidation,
			map[string]string{"name": "is required"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var got seen
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rec := httptest.NewRecorder()
			validatedServer(t, &got).ServeHTTP(rec, req)

			require.Equal(t, tt.status, rec.Code, rec.Body.String())
			body := errorBody(t, rec)
			assert.Equal(t, tt.code, body.Code)
			assert.Equal(t, tt.fields, body.Fields)
			assert.NotEmpty(t, body.RequestID)
			assert.Zero(t, got.calls, "handler must not run")
		})
	}
}

func TestValidatePassesDecodedInputToTheHandler(t *testing.T) {
	var got seen
	id := uuid.NewString()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPut,
		"/things/"+id,
		strings.NewReader(`{"name":"Acme"}`),
	)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+validToken)
	rec := httptest.NewRecorder()
	validatedServer(t, &got).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, seen{calls: 1, id: id, name: "Acme"}, got)
}

func TestWriteErrorMapsFieldErrorsToValidation(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	response.WriteError(rec, req, nil, errors.Join(errors.New("wrapped"), fieldErr{"type": "must be one of"}))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := errorBody(t, rec)
	assert.Equal(t, response.CodeValidation, body.Code)
	assert.Equal(t, map[string]string{"type": "must be one of"}, body.Fields)
}
