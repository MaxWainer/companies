package httpserver

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	healthmodel "example.com/companies/internal/platform/health/model"
	"example.com/companies/internal/platform/httpserver/requestctx"
	"example.com/companies/internal/platform/httpserver/response"
)

const (
	pathLivez    = "/livez"
	pathHealthz  = "/healthz"
	pathReadyz   = "/readyz"
	pathStartupz = "/startupz"
	pathMetrics  = "/metrics"
)

type Prober interface {
	Live() (healthmodel.Report, bool)
	Ready() (healthmodel.Report, bool)
	Started() (healthmodel.Report, bool)
}

type alwaysUp struct{}

func (alwaysUp) Live() (healthmodel.Report, bool)    { return up() }
func (alwaysUp) Ready() (healthmodel.Report, bool)   { return up() }
func (alwaysUp) Started() (healthmodel.Report, bool) { return up() }

func up() (healthmodel.Report, bool) { return healthmodel.Report{Status: healthmodel.StatusUp}, true }

func operationalRoutes(opts Options) []Route {
	p := probes{opts.Probes}
	if p.Prober == nil {
		p.Prober = alwaysUp{}
	}
	metrics := promhttp.HandlerFor(opts.Registry, promhttp.HandlerOpts{
		Registry:          opts.Registry,
		EnableOpenMetrics: true,
	})
	return []Route{
		{Method: http.MethodGet, Path: pathLivez, Public: true, Handler: p.livez},
		{Method: http.MethodGet, Path: pathHealthz, Public: true, Handler: p.livez},
		{Method: http.MethodGet, Path: pathReadyz, Public: true, Handler: p.readyz},
		{Method: http.MethodGet, Path: pathStartupz, Public: true, Handler: p.startupz},
		{Method: http.MethodGet, Path: pathMetrics, Public: true, Handler: metrics.ServeHTTP},
	}
}

type probes struct{ Prober }

// @Summary		Liveness probe
// @Description	The process answers; never depends on PostgreSQL or Kafka.
// @Tags			operations
// @Produce		json
// @Success		200	{object}	healthmodel.Report
// @Failure		503	{object}	healthmodel.Report
// @Router			/livez [get]
// @Router			/healthz [get]
func (p probes) livez(w http.ResponseWriter, r *http.Request) { writeReport(w, r, p.Live) }

// @Summary		Readiness probe
// @Description	503 when PostgreSQL is unreachable or the server is shutting down; an unreachable Kafka only makes it "degraded" (still 200). Check errors and details are returned only with a valid bearer token.
// @Tags			operations
// @Produce		json
// @Success		200	{object}	healthmodel.Report
// @Failure		503	{object}	healthmodel.Report
// @Router			/readyz [get]
func (p probes) readyz(w http.ResponseWriter, r *http.Request) { writeReport(w, r, p.Ready) }

// @Summary	Startup probe (first round of checks completed)
// @Tags		operations
// @Produce	json
// @Success	200	{object}	healthmodel.Report
// @Failure	503	{object}	healthmodel.Report
// @Router		/startupz [get]
func (p probes) startupz(w http.ResponseWriter, r *http.Request) { writeReport(w, r, p.Started) }

func writeReport(w http.ResponseWriter, r *http.Request, get func() (healthmodel.Report, bool)) {
	w.Header().Set("Cache-Control", "no-store")
	report, ok := get()
	if !requestctx.AuthFrom(r.Context()).Authenticated {
		report = redact(report)
	}
	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}
	response.JSON(w, status, report)
}

func redact(report healthmodel.Report) healthmodel.Report {
	if len(report.Checks) == 0 {
		return report
	}
	checks := make(map[string]healthmodel.Result, len(report.Checks))
	for name, res := range report.Checks {
		checks[name] = healthmodel.Result{Status: res.Status, Critical: res.Critical, CheckedAt: res.CheckedAt}
	}
	return healthmodel.Report{Status: report.Status, Checks: checks}
}
