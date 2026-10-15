package model

import (
	"maps"
	"slices"
	"testing"

	"github.com/mailru/easyjson"
	"github.com/mailru/easyjson/jlexer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	companymodel "example.com/companies/internal/company/model"
	"example.com/companies/internal/platform/httpserver/request"
)

func decodeBody(body string, dst easyjson.Unmarshaler) error {
	in := jlexer.Lexer{Data: []byte(body)}
	dst.UnmarshalEasyJSON(&in)
	in.Consumed()
	return in.Error()
}

func TestOptionalDecoding(t *testing.T) {
	var req PatchRequest
	require.NoError(t, decodeBody(`{"name":"Acme","description":null}`, &req))

	assert.Equal(t, request.Optional[string]{Value: "Acme", Set: true}, req.Name)
	assert.Equal(t, request.Optional[string]{Set: true, Null: true}, req.Description)
	assert.Equal(t, request.Optional[int]{}, req.AmountOfEmployees, "absent member stays unset")
}

func TestDecodeErrors(t *testing.T) {
	tests := []struct {
		body   string
		field  string
		reason string
	}{
		{`{"registered":"yes"}`, "registered", "must be a JSON boolean"},
		{`{"amount_of_employees":"many"}`, "amount_of_employees", "must be a JSON integer"},
		{`{"amount_of_employees":1.5}`, "amount_of_employees", "must be a JSON integer"},
		{`{"amount_of_employees":1e3}`, "amount_of_employees", "must be a JSON integer"},
		{`{"amount_of_employees":99999999999999999999}`, "amount_of_employees", "must be a JSON integer"},
		{`{"name":42}`, "name", "must be a JSON string"},
		{`{"name":{"nested":true}}`, "name", "must be a JSON string"},
		{`{"type":["COOPERATIVE"]}`, "type", "must be a JSON string"},
		{`{"id":"x"}`, "id", "is not allowed"},
		{`{"name":"a","name1":"b"}`, "name1", "is not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			for _, dst := range []easyjson.Unmarshaler{&CreateRequest{}, &PatchRequest{}} {
				err := decodeBody(tt.body, dst)
				var fe *request.FieldError
				require.ErrorAs(t, err, &fe, "%T", dst)
				assert.Equal(t, tt.field, fe.Field)
				assert.Equal(t, tt.reason, fe.Reason)
			}
		})
	}
}

func TestDecodeRejectsNonObjectsAndMalformedJSON(t *testing.T) {
	for _, body := range []string{`[]`, `"str"`, `42`, `true`} {
		require.ErrorIs(t, decodeBody(body, &CreateRequest{}), request.ErrNotObject, body)
	}
	for _, body := range []string{`{"name":`, `{`, `{"name" "x"}`, `{"name":"x",}`, `{} {}`, `{}x`, `{name:"x"}`} {
		err := decodeBody(body, &PatchRequest{})
		require.Error(t, err, body)
		var fe *request.FieldError
		assert.NotErrorAs(t, err, &fe, "%s: malformed JSON must not look like a field error", body)
	}
}

func TestNullBodyIsEmptyObject(t *testing.T) {
	var req CreateRequest
	require.NoError(t, decodeBody(`null`, &req))
	err := req.Validate()
	var ve *companymodel.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Len(t, ve.Fields, 4, "all required fields reported missing")
}

func TestPatchParamsConversion(t *testing.T) {
	var req PatchRequest
	require.NoError(t, decodeBody(`{"description":"new","amount_of_employees":0,"type":"NON_PROFIT"}`, &req))
	require.NoError(t, req.Validate())
	p := req.Params()

	require.True(t, p.Description.Set)
	require.NotNil(t, p.Description.Value)
	assert.Equal(t, "new", *p.Description.Value)
	assert.Equal(t, companymodel.Some(0), p.AmountOfEmployees, "zero is a value, not absence")
	assert.Equal(t, companymodel.Some(companymodel.TypeNonProfit), p.Type)
	assert.False(t, p.Name.Set)
	assert.False(t, p.Registered.Set)
}

func TestCreateParamsKeepNullDescription(t *testing.T) {
	var req CreateRequest
	require.NoError(t, decodeBody(
		`{"name":"A","description":null,"amount_of_employees":1,"registered":false,"type":"COOPERATIVE"}`, &req))
	require.NoError(t, req.Validate())
	p := req.Params()
	assert.Nil(t, p.Description)
	assert.False(t, p.Registered, "explicit false is accepted for a required boolean")
}

func TestDecodeHandlesEscapesAndUnicode(t *testing.T) {
	var req CreateRequest
	require.NoError(t, decodeBody(`{"name":"Café \"Q\"","description":"line\nbreak — ок"}`, &req))
	assert.Equal(t, `Café "Q"`, *req.Name)
	assert.Equal(t, "line\nbreak — ок", *req.Description)
}

func TestValidateAppliesDomainRules(t *testing.T) {
	var create CreateRequest
	require.NoError(t, decodeBody(
		`{"name":"0123456789abcdef","amount_of_employees":-1,"registered":true,"type":"LLC"}`, &create))
	var ve *companymodel.ValidationError
	require.ErrorAs(t, create.Validate(), &ve)
	assert.Equal(t, []string{"amount_of_employees", "name", "type"}, slices.Sorted(maps.Keys(ve.Fields)))

	var trimmed CreateRequest
	require.NoError(t, decodeBody(
		`{"name":"  0123456789abcde  ","amount_of_employees":1,"registered":true,"type":"NON_PROFIT"}`, &trimmed))
	require.NoError(t, trimmed.Validate(), "the name is checked after trimming, as it will be stored")

	var patch PatchRequest
	require.NoError(t, decodeBody(`{"name":null,"type":"LLC"}`, &patch))
	require.ErrorAs(t, patch.Validate(), &ve)
	assert.Equal(t, map[string]string{"name": "must not be null"}, ve.Fields, "null checks come before domain rules")

	var badType PatchRequest
	require.NoError(t, decodeBody(`{"type":"LLC"}`, &badType))
	require.ErrorAs(t, badType.Validate(), &ve)
	assert.Contains(t, ve.Fields, "type")
}
