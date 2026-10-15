package httpserver

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"example.com/companies/internal/platform/httpserver/middleware"
)

type Route struct {
	Method  string
	Path    string
	Handler http.HandlerFunc
	Public  bool
	Input   *middleware.Input
}

type Options struct {
	Logger       *slog.Logger
	Verifier     middleware.TokenVerifier
	Registry     *prometheus.Registry
	Probes       Prober
	Routes       []Route
	MaxBodyBytes int64

	TrustTraceContext bool
}

const (
	defaultMaxBodyBytes = 64 << 10
	requestTimeout      = 10 * time.Second
)

var operationalPaths = []string{pathLivez, pathHealthz, pathReadyz, pathStartupz, pathMetrics}

func NewHandler(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Registry == nil {
		opts.Registry = prometheus.NewRegistry()
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = defaultMaxBodyBytes
	}

	router := http.NewServeMux()
	routes := append(operationalRoutes(opts), opts.Routes...)
	for _, rt := range routes {
		var h http.Handler = rt.Handler
		if rt.Input != nil {
			h = middleware.Validate(*rt.Input, opts.MaxBodyBytes, opts.Logger)(h)
		}
		if !rt.Public {
			h = middleware.RequireAuth(h, opts.Logger)
		}
		router.Handle(rt.Method+" "+rt.Path, h)
	}

	return middleware.Chain(
		middleware.Tracing(opts.TrustTraceContext, operationalPaths...),
		middleware.RequestID,
		middleware.SecureHeaders,
		middleware.Authenticate(opts.Verifier, opts.Logger),
		middleware.Timeout(requestTimeout),
		middleware.Observe(opts.Logger, opts.Registry, operationalPaths...),
		middleware.Recover(opts.Logger),
	)(router)
}
