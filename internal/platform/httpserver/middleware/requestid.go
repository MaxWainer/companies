package middleware

import (
	"net/http"
	"regexp"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"example.com/companies/internal/platform/httpserver/requestctx"
)

const RequestIDHeader = "X-Request-ID"

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID.MatchString(id) {
			id = uuid.NewString()
		}
		w.Header().Set(RequestIDHeader, id)
		trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("http.request_id", id))
		next.ServeHTTP(w, r.WithContext(requestctx.WithRequestID(r.Context(), id)))
	})
}
