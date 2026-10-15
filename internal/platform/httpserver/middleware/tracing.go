package middleware

import (
	"net/http"
	"slices"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func Tracing(trustIncoming bool, untracedPaths ...string) Middleware {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "http.server",
			otelhttp.WithFilter(func(r *http.Request) bool {
				return !slices.Contains(untracedPaths, r.URL.Path)
			}),
			otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return !trustIncoming }),
		)
	}
}
