package health

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"example.com/companies/internal/platform/health/model"
)

func check(name string, critical bool, err error) Check {
	return Check{Name: name, Critical: critical, Run: func(context.Context) (map[string]string, error) {
		return map[string]string{"k": "v"}, err
	}}
}

func TestReadinessAggregation(t *testing.T) {
	degraded := fmt.Errorf("%w: 3 dead letters", ErrDegraded)
	tests := []struct {
		name   string
		checks []Check
		status model.Status
		ok     bool
	}{
		{"all up", []Check{check("db", true, nil), check("kafka", false, nil)}, model.StatusUp, true},
		{
			"non-critical down degrades",
			[]Check{check("db", true, nil), check("kafka", false, errors.New("unreachable"))},
			model.StatusDegraded,
			true,
		},
		{
			"degraded check",
			[]Check{check("db", true, nil), check("outbox", false, degraded)},
			model.StatusDegraded,
			true,
		},
		{
			"critical down fails",
			[]Check{check("db", true, errors.New("refused")), check("kafka", false, nil)},
			model.StatusDown,
			false,
		},
		{"critical degraded still passes", []Check{check("db", true, degraded)}, model.StatusDegraded, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(Options{Readiness: tt.checks, Timeout: time.Second})
			c.CheckNow(context.Background())
			c.started.Store(true)
			r, ok := c.Ready()
			assert.Equal(t, tt.status, r.Status)
			assert.Equal(t, tt.ok, ok)
			assert.Len(t, r.Checks, len(tt.checks))
		})
	}
}

func TestResultDetails(t *testing.T) {
	c := New(Options{Readiness: []Check{check("db", true, errors.New("refused"))}, Timeout: time.Second})
	c.CheckNow(context.Background())
	c.started.Store(true)
	r, _ := c.Ready()
	res := r.Checks["db"]
	assert.Equal(t, model.StatusDown, res.Status)
	assert.True(t, res.Critical)
	assert.Equal(t, "refused", res.Error)
	assert.Equal(t, map[string]string{"k": "v"}, res.Details)
	assert.False(t, res.CheckedAt.IsZero())
}

func TestNotReadyBeforeFirstRoundAndWhileShuttingDown(t *testing.T) {
	c := New(Options{Readiness: []Check{check("db", true, nil)}, Interval: time.Hour, Timeout: time.Second})
	_, ok := c.Ready()
	assert.False(t, ok, "not ready before the first round")
	_, ok = c.Started()
	assert.False(t, ok)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	require.Eventually(t, func() bool { _, ok := c.Started(); return ok }, time.Second, time.Millisecond)
	_, ok = c.Ready()
	assert.True(t, ok)

	c.ShuttingDown()
	r, ok := c.Ready()
	assert.False(t, ok, "readiness fails immediately on shutdown")
	assert.Contains(t, r.Checks, "shutdown")
	_, live := c.Live()
	assert.True(t, live, "a draining process is still alive")
	cancel()
	<-done
}

func TestSlowChecksAreBoundedByTheTimeout(t *testing.T) {
	slow := Check{Name: "slow", Critical: true, Run: func(ctx context.Context) (map[string]string, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	c := New(Options{Readiness: []Check{slow}, Timeout: 20 * time.Millisecond, Interval: time.Second})

	start := time.Now()
	c.CheckNow(context.Background())
	assert.Less(t, time.Since(start), time.Second, "checks are bounded by the timeout")
	c.started.Store(true)

	r, ok := c.Ready()
	assert.False(t, ok)
	assert.Contains(t, r.Checks["slow"].Error, "deadline exceeded")
}

func TestStatusGauge(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := New(Options{
		Liveness:   []Check{check("relay", true, nil)},
		Readiness:  []Check{check("db", true, errors.New("x")), check("outbox", false, ErrDegraded)},
		Timeout:    time.Second,
		Registerer: reg,
	})
	c.CheckNow(context.Background())
	expected := `
# HELP companies_health_check_status Latest health check result: 1 up, 0.5 degraded, 0 down.
# TYPE companies_health_check_status gauge
companies_health_check_status{check="db",probe="readiness"} 0
companies_health_check_status{check="outbox",probe="readiness"} 0.5
companies_health_check_status{check="relay",probe="liveness"} 1
`
	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expected), "companies_health_check_status"))
}
