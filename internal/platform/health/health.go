package health

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"example.com/companies/internal/platform/health/model"
)

var ErrDegraded = errors.New("degraded")

type Check struct {
	Name     string
	Critical bool
	Run      func(ctx context.Context) (map[string]string, error)
}

type Options struct {
	Liveness   []Check
	Readiness  []Check
	Interval   time.Duration
	Timeout    time.Duration
	Logger     *slog.Logger
	Registerer prometheus.Registerer
}

type Checker struct {
	opts         Options
	mu           sync.RWMutex
	live         map[string]model.Result
	ready        map[string]model.Result
	started      atomic.Bool
	shuttingDown atomic.Bool
	gauge        *prometheus.GaugeVec
	now          func() time.Time
}

func New(opts Options) *Checker {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	c := &Checker{
		opts:  opts,
		live:  map[string]model.Result{},
		ready: map[string]model.Result{},
		now:   time.Now,
		gauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "companies", Name: "health_check_status",
			Help: "Latest health check result: 1 up, 0.5 degraded, 0 down.",
		}, []string{"probe", "check"}),
	}
	if opts.Registerer != nil {
		opts.Registerer.MustRegister(c.gauge)
	}
	return c
}

func (c *Checker) Run(ctx context.Context) {
	c.CheckNow(ctx)
	c.started.Store(true)

	ticker := time.NewTicker(c.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.CheckNow(ctx)
		}
	}
}

func (c *Checker) CheckNow(ctx context.Context) {
	live := c.runAll(ctx, "liveness", c.opts.Liveness)
	ready := c.runAll(ctx, "readiness", c.opts.Readiness)
	c.mu.Lock()
	c.live, c.ready = live, ready
	c.mu.Unlock()
}

func (c *Checker) ShuttingDown() { c.shuttingDown.Store(true) }

func (c *Checker) Live() (model.Report, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return report(c.live)
}

func (c *Checker) Ready() (model.Report, bool) {
	if c.shuttingDown.Load() {
		return model.Report{Status: model.StatusDown, Checks: map[string]model.Result{
			"shutdown": {
				Status:    model.StatusDown,
				Critical:  true,
				Error:     "server is shutting down",
				CheckedAt: c.now().UTC(),
			},
		}}, false
	}
	if !c.started.Load() {
		return model.Report{Status: model.StatusDown}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return report(c.ready)
}

func (c *Checker) Started() (model.Report, bool) {
	if c.started.Load() {
		return model.Report{Status: model.StatusUp}, true
	}
	return model.Report{Status: model.StatusDown}, false
}

func (c *Checker) runAll(ctx context.Context, probe string, checks []Check) map[string]model.Result {
	results := make(map[string]model.Result, len(checks))
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for _, chk := range checks {
		wg.Go(func() {
			res := c.run(ctx, chk)
			mu.Lock()
			results[chk.Name] = res
			mu.Unlock()
			c.gauge.WithLabelValues(probe, chk.Name).Set(statusValue(res.Status))
			if res.Status == model.StatusDown {
				c.opts.Logger.WarnContext(ctx, "health check failed",
					slog.String("probe", probe), slog.String("check", chk.Name), slog.String("error", res.Error))
			}
		})
	}
	wg.Wait()
	return results
}

func (c *Checker) run(ctx context.Context, chk Check) model.Result {
	ctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()
	start := c.now()
	details, err := chk.Run(ctx)
	res := model.Result{
		Status:     model.StatusUp,
		Critical:   chk.Critical,
		CheckedAt:  start.UTC(),
		DurationMS: float64(c.now().Sub(start).Microseconds()) / 1000,
		Details:    details,
	}
	switch {
	case err == nil:
	case errors.Is(err, ErrDegraded):
		res.Status, res.Error = model.StatusDegraded, err.Error()
	default:
		res.Status, res.Error = model.StatusDown, err.Error()
	}
	return res
}

func report(results map[string]model.Result) (model.Report, bool) {
	r := model.Report{Status: model.StatusUp, Checks: results}
	for _, res := range results {
		switch {
		case res.Status == model.StatusDown && res.Critical:
			r.Status = model.StatusDown
		case res.Status != model.StatusUp && r.Status == model.StatusUp:
			r.Status = model.StatusDegraded
		}
	}
	return r, r.Status != model.StatusDown
}

func statusValue(s model.Status) float64 {
	switch s {
	case model.StatusUp:
		return 1
	case model.StatusDegraded:
		return 0.5
	default:
		return 0
	}
}
