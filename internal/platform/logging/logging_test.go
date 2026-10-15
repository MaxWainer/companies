package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lognoop "go.opentelemetry.io/otel/log/noop"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"

	"example.com/companies/internal/platform/config"
)

const (
	testTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	testSpanID  = "00f067aa0ba902b7"
)

func spanContext(tb testing.TB) context.Context {
	tb.Helper()
	traceID, err := trace.TraceIDFromHex(testTraceID)
	require.NoError(tb, err)
	spanID, err := trace.SpanIDFromHex(testSpanID)
	require.NoError(tb, err)
	sc := trace.NewSpanContext(
		trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled},
	)
	return trace.ContextWithSpanContext(context.Background(), sc)
}

func jsonLogger(t *testing.T, buf *bytes.Buffer, level string) *slog.Logger {
	t.Helper()
	l, err := newLogger(buf, config.Log{Level: level, Format: "json"}, "svc", nil)
	require.NoError(t, err)
	return l
}

func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.Lines(buf.String()) {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m))
		out = append(out, m)
	}
	return out
}

func TestContextAddsTraceFields(t *testing.T) {
	var buf bytes.Buffer
	jsonLogger(t, &buf, "info").InfoContext(spanContext(t), "hello", slog.String("k", "v"))

	line := lines(t, &buf)[0]
	assert.Equal(t, testTraceID, line["trace_id"])
	assert.Equal(t, testSpanID, line["span_id"])
	assert.Equal(t, "v", line["k"])
	assert.Equal(t, "svc", line["service"])
	assert.Contains(t, line, "source", "AddSource is on")
}

func TestTraceFieldsSurviveWith(t *testing.T) {
	var buf bytes.Buffer
	Named(jsonLogger(t, &buf, "info"), "http").WarnContext(spanContext(t), "bound")
	line := lines(t, &buf)[0]
	assert.Equal(t, testTraceID, line["trace_id"])
	assert.Equal(t, "http", line["logger"])
}

func TestNoSpanAddsNothing(t *testing.T) {
	var buf bytes.Buffer
	jsonLogger(t, &buf, "info").InfoContext(context.Background(), "plain")
	assert.NotContains(t, lines(t, &buf)[0], "trace_id")
}

func TestLevelFiltersOutput(t *testing.T) {
	var buf bytes.Buffer
	l := jsonLogger(t, &buf, "warn")
	l.InfoContext(context.Background(), "dropped")
	l.ErrorContext(context.Background(), "kept")
	got := lines(t, &buf)
	require.Len(t, got, 1)
	assert.Equal(t, "kept", got[0]["msg"])
	assert.Equal(t, "ERROR", got[0]["level"])
}

func TestNewValidatesLevelAndBuildsBothFormats(t *testing.T) {
	_, err := New(config.Log{Level: "loud", Format: "json"}, "svc", nil)
	require.ErrorContains(t, err, "parse log level")

	for _, format := range []string{"json", "console"} {
		logger, err := New(config.Log{Level: "warn", Format: format}, "svc", lognoop.NewLoggerProvider())
		require.NoError(t, err, format)
		assert.False(t, logger.Enabled(context.Background(), slog.LevelInfo), "%s: below the level", format)
		assert.True(t, logger.Enabled(context.Background(), slog.LevelWarn),
			"%s: the noop OTel provider is disabled, stdout still takes it", format)
	}
}

func TestOTelHandlerHonoursBothLevels(t *testing.T) {
	exp := &recordingExporter{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exp)))
	var buf bytes.Buffer
	logger, err := newLogger(&buf, config.Log{Level: "warn", Format: "json"}, "svc", lp)
	require.NoError(t, err)

	ctx := spanContext(t)
	logger.InfoContext(ctx, "dropped")
	logger.WarnContext(ctx, "kept")
	logger.With(slog.String("k", "v")).ErrorContext(ctx, "kept too")

	assert.Equal(t, []string{"kept", "kept too"}, exp.bodies())
	assert.Len(t, lines(t, &buf), 2, "stdout gets the same records")
	assert.Equal(t, testTraceID, exp.traceID(0), "the bridge carries the span context")
}

type recordingExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (e *recordingExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range records {
		e.records = append(e.records, r.Clone())
	}
	return nil
}

func (*recordingExporter) Shutdown(context.Context) error   { return nil }
func (*recordingExporter) ForceFlush(context.Context) error { return nil }

func (e *recordingExporter) bodies() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, 0, len(e.records))
	for _, r := range e.records {
		out = append(out, r.Body().AsString())
	}
	return out
}

func (e *recordingExporter) traceID(i int) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.records[i].TraceID().String()
}
