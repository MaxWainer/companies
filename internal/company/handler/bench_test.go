package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/companies/internal/company/model"
)

const benchCreateBody = `{"name":"Acme Holdings","description":"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",` +
	`"amount_of_employees":4200,"registered":true,"type":"CORPORATIONS"}`

type fixedService struct{ c model.Company }

func (s fixedService) Create(context.Context, string, model.CreateParams) (model.Company, error) {
	return s.c, nil
}
func (s fixedService) Get(context.Context, uuid.UUID) (model.Company, error) { return s.c, nil }
func (s fixedService) Patch(context.Context, string, uuid.UUID, model.PatchParams) (model.Company, error) {
	return s.c, nil
}
func (fixedService) Delete(context.Context, string, uuid.UUID) error { return nil }

func BenchmarkHandler(b *testing.B) {
	desc := strings.Repeat("Lorem ipsum ", 10)
	stored := model.Company{
		ID: uuid.New(), Name: "Acme", Description: &desc, AmountOfEmployees: 10,
		Registered: true, Type: model.TypeCorporations, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	h := newTestHandler(fixedService{stored})
	path := "/v1/companies/" + stored.ID.String()

	cases := []struct {
		name, method, path, body string
		auth                     bool
		want                     int
	}{
		{"GET", http.MethodGet, path, "", false, http.StatusOK},
		{"POST", http.MethodPost, "/v1/companies", benchCreateBody, true, http.StatusCreated},
		{"PATCH", http.MethodPatch, path, `{"description":null,"amount_of_employees":11}`, true, http.StatusOK},
		{"DELETE", http.MethodDelete, path, "", true, http.StatusNoContent},
		{"POST_invalid", http.MethodPost, "/v1/companies", `{"name":"Acme"}`, true, http.StatusBadRequest},
		{"POST_unauthorized", http.MethodPost, "/v1/companies", benchCreateBody, false, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					req := httptest.NewRequestWithContext(
						b.Context(),
						tc.method,
						tc.path,
						strings.NewReader(tc.body),
					)
					req.Header.Set("Content-Type", "application/json")
					if tc.auth {
						req.Header.Set("Authorization", "Bearer "+validToken)
					}
					rec := httptest.NewRecorder()
					h.ServeHTTP(rec, req)
					if rec.Code != tc.want {
						b.Fatalf("status %d, want %d", rec.Code, tc.want)
					}
				}
			})
		})
	}
}
