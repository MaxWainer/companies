package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/multitracer"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"example.com/companies/internal/platform/config"
)

//go:embed migrations/*.sql
var migrations embed.FS

type PoolOption func(*pgxpool.Config)

func WithTracing() PoolOption {
	return func(pc *pgxpool.Config) {
		addTracer(pc, otelpgx.NewTracer(otelpgx.WithDisableAcquireTracer()))
	}
}

func WithQueryLog(logger *slog.Logger) PoolOption {
	return func(pc *pgxpool.Config) {
		addTracer(pc, queryLogger{logger: logger, now: time.Now})
	}
}

func addTracer(pc *pgxpool.Config, t pgx.QueryTracer) {
	if pc.ConnConfig.Tracer == nil {
		pc.ConnConfig.Tracer = t
		return
	}
	pc.ConnConfig.Tracer = multitracer.New(pc.ConnConfig.Tracer, t)
}

func Connect(ctx context.Context, cfg config.Database, opts ...PoolOption) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse database dsn: %w", err)
	}
	pc.MaxConns = cfg.MaxConns
	for _, opt := range opts {
		opt(pc)
	}

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]*goose.MigrationResult, error) {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	provider, err := MigrationProvider(db)
	if err != nil {
		return nil, err
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	return results, nil
}

func MigrationProvider(db *sql.DB) (*goose.Provider, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("open migrations: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("create migration locker: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, sub, goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("create migration provider: %w", err)
	}
	return provider, nil
}
