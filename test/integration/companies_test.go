//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"example.com/companies/internal/event/eventpb"
	"example.com/companies/internal/event/kafka"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"example.com/companies/internal/auth/jwt"
	companymodel "example.com/companies/internal/company/model"
)

type apiResponse struct {
	status int
	header http.Header
	body   map[string]any
}

func (r apiResponse) id(t *testing.T) string {
	t.Helper()
	id, ok := r.body["id"].(string)
	require.True(t, ok, "response has no id: %v", r.body)
	return id
}

func call(t *testing.T, method, path, token string, body any) apiResponse {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, ok := body.(string)
		if !ok {
			b, err := json.Marshal(body)
			require.NoError(t, err)
			raw = string(b)
		}
		reader = strings.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, env.baseURL+path, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	out := apiResponse{status: resp.StatusCode, header: resp.Header}
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	if len(bytes.TrimSpace(data)) > 0 {
		require.NoError(t, json.Unmarshal(data, &out.body), "body: %s", data)
	}
	return out
}

func token(t *testing.T, subject string) string {
	t.Helper()
	tok, err := jwt.Issue(env.cfg.Auth, subject, time.Minute, time.Now())
	require.NoError(t, err)
	return tok
}

func uniqueName(t *testing.T) string {
	t.Helper()
	b := make([]byte, 5)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return "co-" + hex.EncodeToString(b)
}

func newCompany(name string) map[string]any {
	return map[string]any{
		"name":                name,
		"description":         "A test company",
		"amount_of_employees": 42,
		"registered":          true,
		"type":                "CORPORATIONS",
	}
}

func fieldErrors(t *testing.T, r apiResponse) map[string]any {
	t.Helper()
	e, ok := r.body["error"].(map[string]any)
	require.True(t, ok, "expected error body, got %v", r.body)
	f, _ := e["fields"].(map[string]any)
	return f
}

func TestCompanyLifecycle(t *testing.T) {
	tok := token(t, "alice")
	name := uniqueName(t)

	created := call(t, http.MethodPost, "/v1/companies", tok, newCompany(name))
	require.Equal(t, http.StatusCreated, created.status, created.body)
	id := created.id(t)
	require.NotEmpty(t, id)
	assert.Equal(t, "/v1/companies/"+id, created.header.Get("Location"))
	assert.Equal(t, name, created.body["name"])
	assert.Equal(t, "A test company", created.body["description"])
	assert.EqualValues(t, 42, created.body["amount_of_employees"])
	assert.Equal(t, true, created.body["registered"])
	assert.Equal(t, "CORPORATIONS", created.body["type"])

	got := call(t, http.MethodGet, "/v1/companies/"+id, "", nil)
	require.Equal(t, http.StatusOK, got.status)
	assert.Equal(t, created.body, got.body, "GET returns exactly what POST returned")

	patched := call(t, http.MethodPatch, "/v1/companies/"+id, tok, map[string]any{
		"description": nil,
		"type":        "SOLE_PROPRIETORSHIP",
	})
	require.Equal(t, http.StatusOK, patched.status, patched.body)
	assert.Nil(t, patched.body["description"])
	assert.Equal(t, "SOLE_PROPRIETORSHIP", patched.body["type"])
	assert.Equal(t, name, patched.body["name"], "untouched fields are preserved")
	assert.Equal(t, created.body["created_at"], patched.body["created_at"])
	assert.NotEqual(t, created.body["updated_at"], patched.body["updated_at"])

	got = call(t, http.MethodGet, "/v1/companies/"+id, "", nil)
	assert.Equal(t, patched.body, got.body)

	deleted := call(t, http.MethodDelete, "/v1/companies/"+id, tok, nil)
	assert.Equal(t, http.StatusNoContent, deleted.status)

	assert.Equal(t, http.StatusNotFound, call(t, http.MethodGet, "/v1/companies/"+id, "", nil).status)
	assert.Equal(t, http.StatusNotFound, call(t, http.MethodDelete, "/v1/companies/"+id, tok, nil).status)
	assert.Equal(t, http.StatusNotFound,
		call(t, http.MethodPatch, "/v1/companies/"+id, tok, map[string]any{"registered": false}).status)
}

func TestAuthentication(t *testing.T) {
	body := newCompany(uniqueName(t))

	assert.Equal(t, http.StatusUnauthorized, call(t, http.MethodPost, "/v1/companies", "", body).status)

	forged := env.cfg.Auth
	forged.JWTSecret = "some-other-secret-0123456789abcdef!!"
	bad, err := jwt.Issue(forged, "mallory", time.Minute, time.Now())
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, call(t, http.MethodPost, "/v1/companies", bad, body).status)

	expired, err := jwt.Issue(env.cfg.Auth, "alice", time.Minute, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, call(t, http.MethodPost, "/v1/companies", expired, body).status)

	wrongIssuer := env.cfg.Auth
	wrongIssuer.Issuer = "someone-else"
	other, err := jwt.Issue(wrongIssuer, "alice", time.Minute, time.Now())
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, call(t, http.MethodPost, "/v1/companies", other, body).status)
}

func TestNameUniqueness(t *testing.T) {
	tok := token(t, "alice")
	name := uniqueName(t)

	require.Equal(t, http.StatusCreated, call(t, http.MethodPost, "/v1/companies", tok, newCompany(name)).status)

	dup := call(t, http.MethodPost, "/v1/companies", tok, newCompany(strings.ToUpper(name)))
	assert.Equal(t, http.StatusConflict, dup.status, "uniqueness is case-insensitive")
	assert.Contains(t, fieldErrors(t, dup), "name")

	other := call(t, http.MethodPost, "/v1/companies", tok, newCompany(uniqueName(t)))
	require.Equal(t, http.StatusCreated, other.status)
	rename := call(t, http.MethodPatch, "/v1/companies/"+other.id(t), tok, map[string]any{"name": name})
	assert.Equal(t, http.StatusConflict, rename.status)
}

func TestValidation(t *testing.T) {
	tok := token(t, "alice")
	with := func(key string, value any) map[string]any {
		c := newCompany(uniqueName(t))
		if value == nil {
			delete(c, key)
		} else {
			c[key] = value
		}
		return c
	}

	tests := []struct {
		name  string
		body  any
		field string
	}{
		{"name too long", with("name", strings.Repeat("x", companymodel.MaxNameLength+1)), "name"},
		{"blank name", with("name", "   "), "name"},
		{"missing name", with("name", nil), "name"},
		{"missing registered", with("registered", nil), "registered"},
		{"missing employees", with("amount_of_employees", nil), "amount_of_employees"},
		{"negative employees", with("amount_of_employees", -1), "amount_of_employees"},
		{"employees overflow", with("amount_of_employees", int64(1)<<40), "amount_of_employees"},
		{"unknown type", with("type", "LLC"), "type"},
		{
			"description too long",
			with("description", strings.Repeat("d", companymodel.MaxDescriptionLength+1)),
			"description",
		},
		{"client supplied id", with("id", "8b0e7c2c-6b7a-4c39-9a3e-6c1d8a1b2c3d"), "id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := call(t, http.MethodPost, "/v1/companies", tok, tt.body)
			assert.Equal(t, http.StatusBadRequest, r.status, r.body)
			assert.Contains(t, fieldErrors(t, r), tt.field)
		})
	}

	t.Run("limits count characters, not bytes", func(t *testing.T) {
		b := make([]byte, 3)
		_, _ = rand.Read(b)
		name := "Ёжик-" + hex.EncodeToString(b) + "-ЯЮЭ"
		require.Len(t, []rune(name), companymodel.MaxNameLength)
		r := call(t, http.MethodPost, "/v1/companies", tok, newCompany(name))
		assert.Equal(t, http.StatusCreated, r.status, r.body)
	})

	t.Run("patch validation", func(t *testing.T) {
		c := call(t, http.MethodPost, "/v1/companies", tok, newCompany(uniqueName(t)))
		require.Equal(t, http.StatusCreated, c.status)
		path := "/v1/companies/" + c.id(t)

		r := call(t, http.MethodPatch, path, tok, map[string]any{"name": nil})
		assert.Equal(t, http.StatusBadRequest, r.status)
		r = call(t, http.MethodPatch, path, tok, map[string]any{"id": "x"})
		assert.Equal(t, http.StatusBadRequest, r.status)
		r = call(t, http.MethodPatch, path, tok, `{"type":"Nope"}`)
		assert.Equal(t, http.StatusBadRequest, r.status)
	})

	t.Run("invalid id", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, call(t, http.MethodGet, "/v1/companies/123", "", nil).status)
	})
}

func TestEventsArePublishedInOrder(t *testing.T) {
	tok := token(t, "event-tester")

	created := call(t, http.MethodPost, "/v1/companies", tok, newCompany(uniqueName(t)))
	require.Equal(t, http.StatusCreated, created.status)
	id := created.id(t)
	path := "/v1/companies/" + id

	require.Equal(
		t,
		http.StatusOK,
		call(t, http.MethodPatch, path, tok, map[string]any{"amount_of_employees": 7}).status,
	)
	require.Equal(
		t,
		http.StatusOK,
		call(t, http.MethodPatch, path, tok, map[string]any{"amount_of_employees": 7}).status,
	)
	require.Equal(t, http.StatusNoContent, call(t, http.MethodDelete, path, tok, nil).status)

	events := readEvents(t, id, 3)
	require.Len(t, events, 3)

	assert.Equal(t, eventpb.EventType_EVENT_TYPE_COMPANY_CREATED, events[0].GetType())
	assert.Equal(t, eventpb.EventType_EVENT_TYPE_COMPANY_UPDATED, events[1].GetType())
	assert.Equal(t, []string{"amount_of_employees"}, events[1].GetChangedFields())
	assert.EqualValues(t, 7, events[1].GetCompany().GetAmountOfEmployees())
	assert.Equal(t, eventpb.EventType_EVENT_TYPE_COMPANY_DELETED, events[2].GetType())
	for _, ev := range events {
		assert.Equal(t, id, ev.GetCompany().GetId())
		assert.Equal(t, "event-tester", ev.GetActor())
	}
}

func readEvents(t *testing.T, companyID string, n int) []*eventpb.CompanyEvent {
	t.Helper()
	var events []*eventpb.CompanyEvent
	for _, msg := range readMessages(t, companyID, n) {
		ev := decodeEvent(t, msg.Value)
		assert.Equal(t, ev.GetId(), header(msg, kafka.HeaderEventID))
		assert.Equal(t, "application/x-protobuf", header(msg, kafka.HeaderContentType))
		assert.NotEmpty(t, header(msg, kafka.HeaderEventType))
		events = append(events, ev)
	}
	return events
}

func decodeEvent(t *testing.T, data []byte) *eventpb.CompanyEvent {
	t.Helper()
	var ev eventpb.CompanyEvent
	require.NoError(t, proto.Unmarshal(data, &ev))
	return &ev
}

func consume(t *testing.T, fn func(kafkago.Message) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	r := kafkago.NewReader(
		kafkago.ReaderConfig{Brokers: env.brokers, Topic: topic, Partition: 0, MaxWait: 200 * time.Millisecond},
	)
	defer r.Close()
	require.NoError(t, r.SetOffset(kafkago.FirstOffset))

	for {
		msg, err := r.ReadMessage(ctx)
		require.NoError(t, err, "timed out waiting for messages")
		if !fn(msg) {
			return
		}
	}
}

func header(msg kafkago.Message, key string) string {
	for _, h := range msg.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func TestOperationalEndpoints(t *testing.T) {
	assert.Equal(t, http.StatusOK, call(t, http.MethodGet, "/healthz", "", nil).status)
	assert.Equal(t, http.StatusOK, call(t, http.MethodGet, "/readyz", "", nil).status)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, env.baseURL+"/metrics", http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "companies_http_request_duration_seconds")
	assert.Contains(t, string(body), "companies_events_published_total")
}
