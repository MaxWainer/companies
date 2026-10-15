package postgres

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	slowQueryThreshold                = 200 * time.Millisecond
	integrityConstraintViolationClass = "23"
)

type queryLogger struct {
	logger *slog.Logger
	now    func() time.Time
}

type queryStartKey struct{}

type queryStart struct {
	sql string
	at  time.Time
}

var _ pgx.QueryTracer = queryLogger{}

func (l queryLogger) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryStartKey{}, queryStart{sql: data.SQL, at: l.now()})
}

func (l queryLogger) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	start, ok := ctx.Value(queryStartKey{}).(queryStart)
	if !ok {
		return
	}
	elapsed := l.now().Sub(start.at)
	attrs := []slog.Attr{slog.String("sql", start.sql), slog.Duration("duration", elapsed)}
	err := data.Err
	switch {
	case err != nil && isConstraintViolation(err):
		l.logger.LogAttrs(
			ctx,
			slog.LevelDebug,
			"query rejected by constraint",
			append(attrs, slog.Any("error", err))...)
	case err != nil && !errors.Is(err, context.Canceled):
		l.logger.LogAttrs(ctx, slog.LevelError, "query failed", append(attrs, slog.Any("error", err))...)
	case err == nil && elapsed > slowQueryThreshold:
		l.logger.LogAttrs(ctx, slog.LevelWarn, "slow query",
			append(attrs, slog.Int64("rows", data.CommandTag.RowsAffected()))...)
	}
}

func isConstraintViolation(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && strings.HasPrefix(pgErr.Code, integrityConstraintViolationClass)
}
