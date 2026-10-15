package app

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"example.com/companies/internal/platform/config"
	"example.com/companies/internal/platform/health"
)

func TestShutdownCancelsRequestsThatOutliveTheDrain(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	a := &App{
		cfg:    config.Config{HTTP: config.HTTP{ShutdownTimeout: 100 * time.Millisecond}},
		logger: slog.New(slog.DiscardHandler),
		health: health.New(health.Options{Interval: time.Hour, Timeout: time.Second}),
		handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
			close(canceled)
		}),
	}
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, ln) }()
	go func() {
		req, _ := http.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			"http://"+ln.Addr().String(),
			http.NoBody,
		)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started
	stop()

	select {
	case err := <-done:
		require.ErrorContains(t, err, "http shutdown")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the shutdown timeout")
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("the stuck request kept running, so it would still hold its DB connection when the pool closes")
	}
}

func TestShutdownLetsInFlightRequestsFinish(t *testing.T) {
	started := make(chan struct{})
	a := &App{
		cfg:    config.Config{HTTP: config.HTTP{ShutdownTimeout: 5 * time.Second}},
		logger: slog.New(slog.DiscardHandler),
		health: health.New(health.Options{Interval: time.Hour, Timeout: time.Second}),
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			select {
			case <-time.After(300 * time.Millisecond):
				w.WriteHeader(http.StatusOK)
			case <-r.Context().Done():
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		}),
	}
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, ln) }()
	status := make(chan int, 1)
	go func() {
		req, _ := http.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			"http://"+ln.Addr().String(),
			http.NoBody,
		)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			status <- 0
			return
		}
		_ = resp.Body.Close()
		status <- resp.StatusCode
	}()
	<-started
	stop()

	require.Equal(t, http.StatusOK, <-status, "the signal does not cancel requests that are already running")
	require.NoError(t, <-done)
	_, ready := a.health.Ready()
	require.False(t, ready, "readiness fails as soon as shutdown starts")
}
