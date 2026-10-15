package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"example.com/companies/internal/auth/jwt"
	authmodel "example.com/companies/internal/auth/model"
	"example.com/companies/internal/company/model"
	"example.com/companies/internal/company/service"
	"example.com/companies/internal/mocks"
	"example.com/companies/internal/platform/httpserver"
	"example.com/companies/internal/platform/httpserver/response"
)

const validToken = "valid-token"

type stubVerifier struct{}

func (stubVerifier) Verify(token string) (*authmodel.Principal, error) {
	if token == validToken {
		return &authmodel.Principal{Subject: "alice"}, nil
	}
	return nil, jwt.ErrInvalidToken
}

type result struct {
	status int
	header http.Header
	body   map[string]any
}

func do(t *testing.T, h http.Handler, method, path, token, contentType, body string) result {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	res := result{status: rec.Code, header: rec.Header()}
	if rec.Body.Len() > 0 && strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res.body))
	}
	return res
}

func errorFields(t *testing.T, r result) map[string]any {
	t.Helper()
	e, ok := r.body["error"].(map[string]any)
	require.True(t, ok, "error envelope expected, got %v", r.body)
	fields, _ := e["fields"].(map[string]any)
	return fields
}

func newTestHandler(svc service.Companies) http.Handler {
	return httpserver.NewHandler(httpserver.Options{
		Verifier:     stubVerifier{},
		Routes:       New(svc, nil).Routes(),
		MaxBodyBytes: 1 << 10,
	})
}

func newMockedHandler(t *testing.T) (http.Handler, *mocks.MockCompanies) {
	t.Helper()
	svc := mocks.NewMockCompanies(gomock.NewController(t))
	return newTestHandler(svc), svc
}

const validBody = `{"name":"Acme","amount_of_employees":3,"registered":true,"type":"CORPORATIONS"}`

func TestMutationsRequireAuthentication(t *testing.T) {
	h, _ := newMockedHandler(t)
	id := uuid.NewString()
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/v1/companies"},
		{http.MethodPatch, "/v1/companies/" + id},
		{http.MethodDelete, "/v1/companies/" + id},
	} {
		for _, token := range []string{"", "forged"} {
			r := do(t, h, tc.method, tc.path, token, "application/json", validBody)
			assert.Equal(t, http.StatusUnauthorized, r.status, "%s %s token=%q", tc.method, tc.path, token)
			assert.Contains(t, r.header.Get("WWW-Authenticate"), "Bearer")
		}
	}
}

func TestGetIsPublic(t *testing.T) {
	h, svc := newMockedHandler(t)
	id := uuid.New()
	svc.EXPECT().Get(gomock.Any(), id).Return(model.Company{ID: id, Name: "Acme", Type: model.TypeCooperative}, nil)

	r := do(t, h, http.MethodGet, "/v1/companies/"+id.String(), "", "", "")
	assert.Equal(t, http.StatusOK, r.status)
	assert.Equal(t, id.String(), r.body["id"])
	assert.Contains(t, r.body, "description", "description is always present, null when unset")
	assert.NotEmpty(t, r.header.Get("X-Request-ID"))
}

func TestCreate(t *testing.T) {
	h, svc := newMockedHandler(t)
	id := uuid.New()
	svc.EXPECT().
		Create(gomock.Any(), "alice", model.CreateParams{
			Name: "Acme", AmountOfEmployees: 3, Registered: true, Type: model.TypeCorporations,
		}).
		Return(model.Company{ID: id, Name: "Acme"}, nil)

	r := do(t, h, http.MethodPost, "/v1/companies", validToken, "application/json; charset=utf-8", validBody)
	require.Equal(t, http.StatusCreated, r.status)
	assert.Equal(t, "/v1/companies/"+id.String(), r.header.Get("Location"))
}

func TestCreateRejectsBadInputWithoutCallingTheService(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		status      int
		fields      []string
	}{
		{
			"missing required fields", "application/json", `{"description":"x"}`, 400,
			[]string{"name", "amount_of_employees", "registered", "type"},
		},
		{"unknown field", "application/json", `{"id":"x","name":"Acme"}`, 400, []string{"id"}},
		{
			"wrong type", "application/json",
			`{"name":"Acme","amount_of_employees":"many","registered":true,"type":"COOPERATIVE"}`,
			400,
			[]string{"amount_of_employees"},
		},
		{
			"fractional employees", "application/json",
			`{"name":"Acme","amount_of_employees":1.5,"registered":true,"type":"COOPERATIVE"}`,
			400,
			[]string{"amount_of_employees"},
		},
		{"malformed json", "application/json", `{"name":`, 400, nil},
		{"empty body", "application/json", ``, 400, nil},
		{"array body", "application/json", `[]`, 400, nil},
		{"trailing data", "application/json", validBody + `{}`, 400, nil},
		{"wrong content type", "text/plain", validBody, 415, nil},
		{"too large", "application/json", `{"description":"` + strings.Repeat("x", 2<<10) + `"}`, 413, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newMockedHandler(t)
			r := do(t, h, http.MethodPost, "/v1/companies", validToken, tt.contentType, tt.body)
			assert.Equal(t, tt.status, r.status)
			fields := errorFields(t, r)
			for _, f := range tt.fields {
				assert.Contains(t, fields, f)
			}
		})
	}
}

func TestPatchDistinguishesNullFromAbsent(t *testing.T) {
	h, svc := newMockedHandler(t)
	id := uuid.New()
	path := "/v1/companies/" + id.String()
	svc.EXPECT().
		Patch(gomock.Any(), "alice", id, model.PatchParams{
			Description: model.Some[*string](nil),
			Registered:  model.Some(false),
			Type:        model.Some(model.TypeSoleProprietorship),
		}).
		Return(model.Company{ID: id}, nil)

	r := do(t, h, http.MethodPatch, path, validToken, "application/merge-patch+json",
		`{"description":null,"registered":false,"type":"SOLE_PROPRIETORSHIP"}`)
	require.Equal(t, http.StatusOK, r.status)

	r = do(t, h, http.MethodPatch, path, validToken, "application/json", `{"registered":"yes"}`)
	assert.Equal(t, http.StatusBadRequest, r.status)
	assert.Equal(t, map[string]any{"registered": "must be a JSON boolean"}, errorFields(t, r))

	r = do(t, h, http.MethodPatch, path, validToken, "application/json", `{"id":"x"}`)
	assert.Equal(t, http.StatusBadRequest, r.status)
	assert.Equal(t, map[string]any{"id": "is not allowed"}, errorFields(t, r))

	r = do(t, h, http.MethodPatch, path, validToken, "application/json", `{"name":null,"type":null}`)
	assert.Equal(t, http.StatusBadRequest, r.status)
	assert.Equal(t, map[string]any{"name": "must not be null", "type": "must not be null"}, errorFields(t, r))
}

func TestInvalidID(t *testing.T) {
	h, _ := newMockedHandler(t)
	r := do(t, h, http.MethodGet, "/v1/companies/not-a-uuid", "", "", "")
	assert.Equal(t, http.StatusBadRequest, r.status)
	assert.Contains(t, errorFields(t, r), "id")
}

func TestDelete(t *testing.T) {
	h, svc := newMockedHandler(t)
	id := uuid.New()
	svc.EXPECT().Delete(gomock.Any(), "alice", id).Return(nil)

	r := do(t, h, http.MethodDelete, "/v1/companies/"+id.String(), validToken, "", "")
	assert.Equal(t, http.StatusNoContent, r.status)
}

func TestDomainErrorMapping(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{model.ErrNotFound, http.StatusNotFound, response.CodeNotFound},
		{model.ErrNameTaken, http.StatusConflict, response.CodeConflict},
		{model.NewValidationError("name", "bad"), http.StatusBadRequest, response.CodeValidation},
		{errors.New("boom"), http.StatusInternalServerError, response.CodeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			h, svc := newMockedHandler(t)
			svc.EXPECT().Delete(gomock.Any(), gomock.Any(), gomock.Any()).Return(tt.err)

			r := do(t, h, http.MethodDelete, "/v1/companies/"+uuid.NewString(), validToken, "", "")
			assert.Equal(t, tt.status, r.status)
			errBody, ok := r.body["error"].(map[string]any)
			require.True(t, ok, "error envelope: %v", r.body)
			assert.Equal(t, tt.code, errBody["code"])
			if tt.status == http.StatusInternalServerError {
				assert.NotContains(t, errBody["message"], "boom", "internals are not leaked")
			}
		})
	}
}

func TestMetricsExposeRoutes(t *testing.T) {
	h, svc := newMockedHandler(t)
	svc.EXPECT().Get(gomock.Any(), gomock.Any()).Return(model.Company{}, model.ErrNotFound)
	do(t, h, http.MethodGet, "/v1/companies/"+uuid.NewString(), "", "", "")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `route="GET /v1/companies/{id}"`)
}
