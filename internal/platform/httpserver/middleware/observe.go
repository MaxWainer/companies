package middleware

import (
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"example.com/companies/internal/platform/httpserver/requestctx"
)

func Observe(logger *slog.Logger, reg prometheus.Registerer, quietPaths ...string) Middleware {
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "companies",
		Name:      "http_request_duration_seconds",
		Help:      "Duration of HTTP requests by route, method and status code.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "route", "code"})
	reg.MustRegister(duration)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			elapsed := time.Since(start)

			route := r.Pattern
			if route == "" {
				route = "unmatched"
			} else if _, path, ok := strings.Cut(route, " "); ok {
				span := trace.SpanFromContext(r.Context())
				span.SetName(route)
				span.SetAttributes(semconv.HTTPRoute(path))
			}

			observer := duration.WithLabelValues(methodLabel(r.Method), route, strconv.Itoa(rec.Status()))
			sc := trace.SpanContextFromContext(r.Context())
			if eo, ok := observer.(prometheus.ExemplarObserver); ok && sc.IsSampled() {
				eo.ObserveWithExemplar(elapsed.Seconds(), prometheus.Labels{"trace_id": sc.TraceID().String()})
			} else {
				observer.Observe(elapsed.Seconds())
			}

			level := slog.LevelInfo
			if slices.Contains(quietPaths, r.URL.Path) {
				level = slog.LevelDebug
			}
			attrs := []slog.Attr{
				slog.String("method", methodLabel(r.Method)),
				slog.String("path", r.URL.Path),
				slog.String("route", route),
				slog.Int("status", rec.Status()),
				slog.Duration("duration", elapsed),
				slog.String("request_id", requestctx.RequestID(r.Context())),
			}
			if p, ok := requestctx.Principal(r.Context()); ok {
				attrs = append(attrs, slog.String("subject", p.Subject))
			}
			logger.LogAttrs(r.Context(), level, "http request", attrs...)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (s *statusRecorder) Status() int {
	if s.status == 0 {
		return http.StatusOK
	}
	return s.status
}

const otherMethod = "_OTHER"

var knownMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodConnect: true, http.MethodOptions: true,
	http.MethodTrace: true,
}

func methodLabel(m string) string {
	if knownMethods[m] {
		return m
	}
	return otherMethod
}
