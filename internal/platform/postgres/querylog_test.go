package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"example.com/companies/internal/platform/logging/logtest"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func trace(l queryLogger, c *clock, took time.Duration, err error) {
	ctx := l.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  "UPDATE companies SET description = $1",
		Args: []any{"secret text"},
	})
	c.t = c.t.Add(took)
	l.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: err})
}

func TestQueryLoggerLevels(t *testing.T) {
	logger, rec := logtest.New()
	c := &clock{t: time.Now()}
	l := queryLogger{logger: logger, now: c.now}

	trace(l, c, time.Millisecond, nil)
	trace(l, c, time.Millisecond, context.Canceled)
	trace(l, c, time.Millisecond, fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23505"}))
	trace(l, c, time.Millisecond, &pgconn.PgError{Code: "53300", Message: "too many connections"})
	trace(l, c, time.Second, nil)

	entries := rec.Entries()
	require.Len(t, entries, 3)
	assert.Equal(t, "query rejected by constraint", entries[0].Message)
	assert.Equal(t, slog.LevelDebug, entries[0].Level, "a constraint violation is a domain outcome, not a failure")
	assert.Equal(t, "query failed", entries[1].Message)
	assert.Equal(t, slog.LevelError, entries[1].Level)
	assert.Equal(t, "slow query", entries[2].Message)
	assert.Equal(t, slog.LevelWarn, entries[2].Level)

	for _, e := range entries {
		assert.Equal(t, "UPDATE companies SET description = $1", e.Attrs["sql"])
		assert.NotContains(t, fmt.Sprint(e.Attrs), "secret text", "bound values must not reach the logs")
	}
}

func TestQueryLoggerIgnoresQueriesItDidNotStart(t *testing.T) {
	logger, rec := logtest.New()
	queryLogger{logger: logger, now: time.Now}.TraceQueryEnd(context.Background(), nil,
		pgx.TraceQueryEndData{Err: errors.New("boom")})
	assert.Zero(t, rec.Len())
}
