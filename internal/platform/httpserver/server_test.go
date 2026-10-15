package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	authmodel "example.com/companies/internal/auth/model"
	healthmodel "example.com/companies/internal/platform/health/model"
	"example.com/companies/internal/platform/httpserver/middleware"
	"example.com/companies/internal/platform/httpserver/requestctx"
	"example.com/companies/internal/platform/httpserver/response"
	"example.com/companies/internal/platform/httpserver/response/model"
	"example.com/companies/internal/platform/logging/logtest"
)

const validToken = "valid-token"

type stubVerifier struct{}

func (stubVerifier) Verify(token string) (*authmodel.Principal, error) {
	if token == validToken {
		return &authmodel.Principal{Subject: "alice"}, nil
	}
	return nil, errors.New("bad token")
}

func testRoutes() []Route {
	return []Route{
		{
			Method: http.MethodGet,
			Path:   "/public/{id}",
			Public: true,
			Handler: func(w http.ResponseWriter, r *http.Request) {
				p, ok := requestctx.Principal(r.Context())
				_, _ = w.Write([]byte(map[bool]string{true: p.Subject, false: "anonymous"}[ok]))
			},
		},
		{Method: http.MethodDelete, Path: "/private/{id}", Handler: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}},
		{Method: http.MethodGet, Path: "/fail", Public: true, Handler: func(w http.ResponseWriter, r *http.Request) {
			response.WriteError(w, r, slog.New(slog.DiscardHandler), assert.AnError)
		}},
		{Method: http.MethodGet, Path: "/panic", Public: true, Handler: func(http.ResponseWriter, *http.Request) {
			panic("kaboom")
		}},
	}
}

func newTestServer(opts Options) http.Handler {
	opts.Verifier = stubVerifier{}
	opts.Routes = testRoutes()
	return NewHandler(opts)
}

func serve(t *testing.T, h http.Handler, method, path string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, http.NoBody)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) model.ErrorDetail {
	t.Helper()
	var body model.ErrorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	return body.Error
}

func TestNonPublicRoutesRequireAuthentication(t *testing.T) {
	h := newTestServer(Options{})
	tests := map[string]struct {
		header string
		status int
		reason string
	}{
		"no header":        {"", http.StatusUnauthorized, "missing bearer token"},
		"wrong scheme":     {"Basic " + validToken, http.StatusUnauthorized, "missing bearer token"},
		"scheme only":      {"Bearer", http.StatusUnauthorized, "missing bearer token"},
		"forged token":     {"Bearer " + validToken + "x", http.StatusUnauthorized, "invalid or expired token"},
		"valid":            {"Bearer " + validToken, http.StatusNoContent, ""},
		"lowercase scheme": {"bearer " + validToken, http.StatusNoContent, ""},
		"extra whitespace": {"Bearer  " + validToken, http.StatusNoContent, ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rec := serve(t, h, http.MethodDelete, "/private/1", map[string]string{"Authorization": tt.header})
			require.Equal(t, tt.status, rec.Code)
			if tt.reason != "" {
				assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "Bearer")
				body := errorBody(t, rec)
				assert.Equal(t, response.CodeUnauthorized, body.Code)
				assert.Equal(t, tt.reason, body.Message)
			}
		})
	}
}

func TestPublicRoutesSeeOptionalPrincipal(t *testing.T) {
	h := newTestServer(Options{})
	status := func(header map[string]string) string {
		rec := serve(t, h, http.MethodGet, "/public/1", header)
		require.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}
	assert.Equal(t, "anonymous", status(nil))
	assert.Equal(t, "alice", status(map[string]string{"Authorization": "Bearer " + validToken}))
	assert.Equal(t, "anonymous", status(map[string]string{"Authorization": "Bearer forged"}),
		"a bad token on a public route is ignored, not rejected")
}

func TestRequestIDPropagation(t *testing.T) {
	h := newTestServer(Options{})
	send := func(id string) *httptest.ResponseRecorder {
		return serve(t, h, http.MethodDelete, "/private/1", map[string]string{middleware.RequestIDHeader: id})
	}

	rec := send("abc-123.DEF_4")
	assert.Equal(t, "abc-123.DEF_4", rec.Header().Get(middleware.RequestIDHeader), "well-formed ids are echoed")
	assert.Equal(t, "abc-123.DEF_4", errorBody(t, rec).RequestID, "error bodies carry the request id")

	for _, bad := range []string{"", "has space", "<script>", strings.Repeat("a", 129)} {
		got := send(bad).Header().Get(middleware.RequestIDHeader)
		_, err := uuid.Parse(got)
		assert.NoError(t, err, "malformed id %q is replaced with a generated UUID, got %q", bad, got)
	}
}

func TestAccessLog(t *testing.T) {
	logger, logs := logtest.New()
	h := newTestServer(Options{Logger: logger})

	serve(t, h, http.MethodDelete, "/private/42", map[string]string{"Authorization": "Bearer " + validToken})
	entries := logs.Filter("http request")
	require.Len(t, entries, 1)
	fields := entries[0].Attrs
	assert.Equal(t, slog.LevelInfo, entries[0].Level)
	assert.Equal(t, "DELETE /private/{id}", fields["route"], "route is the pattern, not the raw path")
	assert.Equal(t, "/private/42", fields["path"])
	assert.EqualValues(t, http.StatusNoContent, fields["status"])
	assert.Equal(t, "alice", fields["subject"], "authenticated subject is logged")
	assert.NotEmpty(t, fields["request_id"])

	logs.Reset()
	serve(t, h, http.MethodDelete, "/private/42", nil)
	entries = logs.Filter("http request")
	require.Len(t, entries, 1)
	assert.EqualValues(t, http.StatusUnauthorized, entries[0].Attrs["status"])

	logs.Reset()
	serve(t, h, http.MethodGet, "/healthz", nil)
	entries = logs.Filter("http request")
	require.Len(t, entries, 1)
	assert.Equal(t, slog.LevelDebug, entries[0].Level)
}

func TestUnexpectedErrorsAreHidden(t *testing.T) {
	rec := serve(t, newTestServer(Options{}), http.MethodGet, "/fail", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, response.CodeInternal, errorBody(t, rec).Code)
	assert.NotContains(t, rec.Body.String(), assert.AnError.Error(), "internals are not leaked")
}

func TestPanicsBecome500AndAreObserved(t *testing.T) {
	logger, logs := logtest.New()
	h := newTestServer(Options{Logger: logger})
	rec := serve(t, h, http.MethodGet, "/panic", nil)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, response.CodeInternal, errorBody(t, rec).Code)
	require.Len(t, logs.Filter("panic serving request"), 1)
	access := logs.Filter("http request")
	require.Len(t, access, 1, "Recover sits inside Observe, so the panic is in the access log")
	assert.EqualValues(t, http.StatusInternalServerError, access[0].Attrs["status"])
}

type fakeProbes struct{ live, ready, started bool }

func (f *fakeProbes) report(ok bool) (healthmodel.Report, bool) {
	if ok {
		return healthmodel.Report{Status: healthmodel.StatusUp}, true
	}
	return healthmodel.Report{Status: healthmodel.StatusDown, Checks: map[string]healthmodel.Result{
		"postgres": {Status: healthmodel.StatusDown, Critical: true, Error: "db down"},
	}}, false
}
func (f *fakeProbes) Live() (healthmodel.Report, bool)    { return f.report(f.live) }
func (f *fakeProbes) Ready() (healthmodel.Report, bool)   { return f.report(f.ready) }
func (f *fakeProbes) Started() (healthmodel.Report, bool) { return f.report(f.started) }

func TestProbeEndpoints(t *testing.T) {
	probes := &fakeProbes{live: true, ready: true, started: true}
	h := newTestServer(Options{Probes: probes})
	for _, path := range []string{"/livez", "/healthz", "/readyz", "/startupz"} {
		rec := serve(t, h, http.MethodGet, path, nil)
		assert.Equal(t, http.StatusOK, rec.Code, path)
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"), path)
	}

	probes.ready = false
	rec := serve(t, h, http.MethodGet, "/readyz", nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var report healthmodel.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
	assert.Equal(t, healthmodel.StatusDown, report.Status)
	assert.Equal(t, healthmodel.StatusDown, report.Checks["postgres"].Status, "anyone sees which check failed")
	assert.Empty(t, report.Checks["postgres"].Error, "but not why: errors name internal hosts")

	rec = serve(t, h, http.MethodGet, "/readyz", map[string]string{"Authorization": "Bearer " + validToken})
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
	assert.Equal(t, "db down", report.Checks["postgres"].Error, "an authenticated caller gets the details")
	assert.Equal(
		t,
		http.StatusOK,
		serve(t, h, http.MethodGet, "/livez", nil).Code,
		"liveness is independent of readiness",
	)

	probes.started = false
	assert.Equal(t, http.StatusServiceUnavailable, serve(t, h, http.MethodGet, "/startupz", nil).Code)
}

func TestProbesDefaultToUp(t *testing.T) {
	h := newTestServer(Options{})
	for _, path := range []string{"/livez", "/readyz", "/startupz"} {
		assert.Equal(t, http.StatusOK, serve(t, h, http.MethodGet, path, nil).Code, path)
	}
}

func TestMetricsExposeRoutes(t *testing.T) {
	h := newTestServer(Options{})
	serve(t, h, http.MethodGet, "/public/7", nil)
	serve(t, h, http.MethodGet, "/nope", nil)

	rec := serve(t, h, http.MethodGet, "/metrics", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `route="GET /public/{id}"`)
	assert.Contains(t, rec.Body.String(), `route="unmatched"`, "404s are measured without exploding cardinality")
}

func TestUnknownRoutesAndMethods(t *testing.T) {
	h := newTestServer(Options{})
	assert.Equal(t, http.StatusNotFound, serve(t, h, http.MethodGet, "/nope", nil).Code)
	assert.Equal(t, http.StatusMethodNotAllowed, serve(t, h, http.MethodGet, "/private/1", nil).Code)
}

func TestServerSpanIsNamedAfterRoute(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(tracenoop.NewTracerProvider()) })

	h := newTestServer(Options{})
	serve(t, h, http.MethodGet, "/public/1", nil)
	serve(t, h, http.MethodGet, "/healthz", nil)

	spans := recorder.Ended()
	require.Len(t, spans, 1, "probe endpoints are not traced")
	assert.Equal(t, "GET /public/{id}", spans[0].Name())
	assert.Contains(t, spans[0].Attributes(), semconv.HTTPRoute("/public/{id}"))
}

func TestChainOrder(t *testing.T) {
	var order []string
	mw := func(name string) middleware.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := middleware.Chain(mw("a"), mw("b"), mw("c"))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		order = append(order, "handler")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
	assert.Equal(t, []string{"a", "b", "c", "handler"}, order, "the first middleware is the outermost")
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	h := newTestServer(Options{})
	for _, path := range []string{"/public/1", "/nope", "/livez"} {
		rec := serve(t, h, http.MethodGet, path, nil)
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), path)
		assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"), path)
		assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"), path)
	}
}

func TestArbitraryMethodsDoNotCreateMetricSeries(t *testing.T) {
	reg := prometheus.NewRegistry()
	h := newTestServer(Options{Registry: reg})
	for i := range 50 {
		serve(t, h, fmt.Sprintf("BOGUS%d", i), "/public/1", nil)
	}
	families, err := reg.Gather()
	require.NoError(t, err)
	methods := map[string]bool{}
	for _, f := range families {
		if f.GetName() != "companies_http_request_duration_seconds" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "method" {
					methods[l.GetValue()] = true
				}
			}
		}
	}
	assert.Equal(t, map[string]bool{"_OTHER": true}, methods, "unknown methods collapse into one series")
}

func TestIncomingTraceContextIsTrustedOnlyWhenConfigured(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	prop := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(tracenoop.NewTracerProvider())
		otel.SetTextMapPropagator(prop)
	})
	const incoming = "4bf92f3577b34da6a3ce929d0e0e4736"
	header := map[string]string{"traceparent": "00-" + incoming + "-00f067aa0ba902b7-01"}

	serve(t, newTestServer(Options{TrustTraceContext: true}), http.MethodGet, "/public/1", header)
	serve(t, newTestServer(Options{}), http.MethodGet, "/public/1", header)

	spans := recorder.Ended()
	require.Len(t, spans, 2)
	assert.Equal(t, incoming, spans[0].SpanContext().TraceID().String(), "trusted: the trace continues")
	assert.NotEqual(t, incoming, spans[1].SpanContext().TraceID().String(), "public: a new trace starts")
	require.Len(t, spans[1].Links(), 1, "and only links to the caller's span")
	assert.Equal(t, incoming, spans[1].Links()[0].SpanContext.TraceID().String())
}

func TestRequestsGetADeadline(t *testing.T) {
	var (
		remaining   time.Duration
		hasDeadline bool
	)
	h := NewHandler(Options{Verifier: stubVerifier{}, Routes: []Route{{
		Method: http.MethodGet, Path: "/deadline", Public: true,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			var deadline time.Time
			deadline, hasDeadline = r.Context().Deadline()
			remaining = time.Until(deadline)
			w.WriteHeader(http.StatusNoContent)
		},
	}}})
	serve(t, h, http.MethodGet, "/deadline", nil)
	require.True(t, hasDeadline)
	assert.InDelta(t, requestTimeout, remaining, float64(time.Second))
}

func TestSlowRequestsTimeOutWith503(t *testing.T) {
	h := middleware.Timeout(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		response.WriteError(w, r, slog.New(slog.DiscardHandler), r.Context().Err())
	}))
	rec := serve(t, h, http.MethodGet, "/", nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, response.CodeTimeout, errorBody(t, rec).Code)
}
