package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	httpSwagger "github.com/swaggo/http-swagger/v2"
	"golang.org/x/sync/errgroup"

	"example.com/companies/api"
	"example.com/companies/internal/auth/jwt"
	companyhandler "example.com/companies/internal/company/handler"
	companyrepo "example.com/companies/internal/company/repository"
	companyservice "example.com/companies/internal/company/service"
	"example.com/companies/internal/event/kafka"
	"example.com/companies/internal/platform/config"
	"example.com/companies/internal/platform/health"
	"example.com/companies/internal/platform/httpserver"
	"example.com/companies/internal/platform/logging"
	"example.com/companies/internal/platform/postgres"
)

const (
	readHeaderTimeout   = 5 * time.Second
	readTimeout         = 15 * time.Second
	writeTimeout        = 15 * time.Second
	idleTimeout         = 60 * time.Second
	healthCheckInterval = 5 * time.Second
	healthCheckTimeout  = 2 * time.Second
)

type App struct {
	cfg      config.Config
	logger   *slog.Logger
	pool     *pgxpool.Pool
	producer *kafka.Producer
	health   *health.Checker
	handler  http.Handler
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (_ *App, err error) {
	a := &App{cfg: cfg, logger: logger}
	defer func() {
		if err != nil {
			a.Close()
		}
	}()

	a.pool, err = postgres.Connect(ctx, cfg.Database,
		postgres.WithTracing(),
		postgres.WithQueryLog(logging.Named(logger, "postgres")),
	)
	if err != nil {
		return nil, err
	}
	if cfg.Database.MigrateOnStart {
		results, err := postgres.Migrate(ctx, a.pool)
		if err != nil {
			return nil, err
		}
		logger.InfoContext(ctx, "database migrations applied", slog.Int("count", len(results)))
	}
	if cfg.Kafka.CreateTopic {
		if err := kafka.EnsureTopic(ctx, cfg.Kafka); err != nil {
			return nil, err
		}
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	if a.producer, err = kafka.NewProducer(cfg.Kafka, registry); err != nil {
		return nil, err
	}
	kafkaHealth, err := kafka.HealthCheck(cfg.Kafka)
	if err != nil {
		return nil, err
	}
	a.health = health.New(health.Options{
		Readiness:  []health.Check{postgres.HealthCheck(a.pool), kafkaHealth},
		Interval:   healthCheckInterval,
		Timeout:    healthCheckTimeout,
		Logger:     logging.Named(logger, "health"),
		Registerer: registry,
	})
	a.handler = a.newHTTPHandler(registry)
	return a, nil
}

func (a *App) newHTTPHandler(registry *prometheus.Registry) http.Handler {
	logger := logging.Named(a.logger, "http")
	repo := companyrepo.Traced(companyrepo.New(a.pool))
	events := kafka.Traced(a.producer, a.cfg.Kafka.Topic)
	companies := companyservice.Traced(companyservice.New(repo, events))
	routes := companyhandler.New(companies, logger).Routes()
	if a.cfg.HTTP.DocsEnabled {
		routes = append(routes, docsRoutes()...)
	}
	if a.cfg.HTTP.PprofEnabled {
		routes = append(routes, pprofRoutes()...)
	}
	return httpserver.NewHandler(httpserver.Options{
		Logger:            logger,
		Verifier:          jwt.NewVerifier(a.cfg.Auth),
		Registry:          registry,
		Probes:            a.health,
		Routes:            routes,
		TrustTraceContext: a.cfg.HTTP.TrustTraceContext,
	})
}

func (a *App) Run(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           a.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(logging.Named(a.logger, "http.server").Handler(), slog.LevelError),
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		a.logger.InfoContext(ctx, "http server listening", slog.String("addr", ln.Addr().String()))
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		a.health.Run(gctx)
		return nil
	})
	g.Go(func() error {
		<-gctx.Done()
		a.health.ShuttingDown()
		a.logger.InfoContext(ctx, "shutting down", slog.Duration("delay", a.cfg.HTTP.ShutdownDelay))
		time.Sleep(a.cfg.HTTP.ShutdownDelay)
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.cfg.HTTP.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http shutdown: %w", errors.Join(err, srv.Close()))
		}
		return nil
	})
	return g.Wait()
}

func (a *App) Close() {
	if a.producer != nil {
		if err := a.producer.Close(); err != nil {
			a.logger.Warn("closing kafka publisher", slog.Any("error", err))
		}
	}
	if a.pool != nil {
		a.pool.Close()
	}
}

func docsRoutes() []httpserver.Route {
	ui := httpSwagger.Handler(
		httpSwagger.URL(api.SwaggerInfo.BasePath+"docs/doc.json"),
		httpSwagger.PersistAuthorization(true),
	)
	return []httpserver.Route{
		{Method: http.MethodGet, Path: "/docs", Public: true, Handler: func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/docs/index.html", http.StatusMovedPermanently)
		}},
		{Method: http.MethodGet, Path: "/docs/{file...}", Public: true, Handler: ui},
	}
}

func pprofRoutes() []httpserver.Route {
	return []httpserver.Route{
		{Method: http.MethodGet, Path: "/debug/pprof/", Handler: pprof.Index},
		{Method: http.MethodGet, Path: "/debug/pprof/cmdline", Handler: pprof.Cmdline},
		{Method: http.MethodGet, Path: "/debug/pprof/profile", Handler: pprof.Profile},
		{Method: http.MethodGet, Path: "/debug/pprof/symbol", Handler: pprof.Symbol},
		{Method: http.MethodGet, Path: "/debug/pprof/trace", Handler: pprof.Trace},
	}
}
