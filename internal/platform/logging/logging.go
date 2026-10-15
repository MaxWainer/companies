package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"

	"example.com/companies/internal/platform/config"
)

func New(cfg config.Log, serviceName string, lp log.LoggerProvider) (*slog.Logger, error) {
	return newLogger(os.Stdout, cfg, serviceName, lp)
}

func newLogger(w io.Writer, cfg config.Log, serviceName string, lp log.LoggerProvider) (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		return nil, fmt.Errorf("parse log level: %w", err)
	}

	opts := &slog.HandlerOptions{AddSource: true, Level: level}
	var h slog.Handler
	if cfg.Format == "console" {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}
	h = traceHandler{h}
	if lp != nil {
		otel := levelHandler{otelslog.NewHandler("companies", otelslog.WithLoggerProvider(lp)), level}
		h = slog.NewMultiHandler(h, otel)
	}
	return slog.New(h).With(slog.String("service", serviceName)), nil
}

func Named(l *slog.Logger, name string) *slog.Logger {
	return l.With(slog.String("logger", name))
}

type traceHandler struct {
	slog.Handler
}

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r = r.Clone()
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return h.Handler.Handle(ctx, r)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{h.Handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{h.Handler.WithGroup(name)}
}

type levelHandler struct {
	slog.Handler
	min slog.Leveler
}

func (h levelHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.min.Level() && h.Handler.Enabled(ctx, l)
}

func (h levelHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < h.min.Level() {
		return nil
	}
	return h.Handler.Handle(ctx, r)
}

func (h levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return levelHandler{h.Handler.WithAttrs(attrs), h.min}
}

func (h levelHandler) WithGroup(name string) slog.Handler {
	return levelHandler{h.Handler.WithGroup(name), h.min}
}
