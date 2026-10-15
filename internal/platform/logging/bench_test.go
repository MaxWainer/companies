package logging

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"example.com/companies/internal/platform/config"
)

func BenchmarkLog(b *testing.B) {
	l, err := newLogger(io.Discard, config.Log{Level: "info", Format: "json"}, "svc", nil)
	require.NoError(b, err)
	traced := spanContext(b)
	for name, ctx := range map[string]context.Context{"no_span": context.Background(), "with_span": traced} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				l.LogAttrs(ctx, slog.LevelInfo, "http request",
					slog.String("route", "GET /v1/companies/{id}"), slog.Int("status", 200))
			}
		})
	}
	b.Run("filtered_debug", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			l.LogAttrs(traced, slog.LevelDebug, "skipped")
		}
	})
}
