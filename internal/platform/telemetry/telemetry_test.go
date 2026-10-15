package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"example.com/companies/internal/platform/config"
)

func keepGlobals(t *testing.T) {
	t.Helper()
	tp, lp, prop := otel.GetTracerProvider(), otel.GetLoggerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(tp)
		otel.SetLoggerProvider(lp)
		otel.SetTextMapPropagator(prop)
	})
}

func TestSetupDisabledOnlyInstallsPropagator(t *testing.T) {
	keepGlobals(t)
	tel, err := Setup(t.Context(), config.Telemetry{Enabled: false}, "test")
	require.NoError(t, err)
	assert.Nil(t, tel.LoggerProvider)
	assert.ElementsMatch(t, []string{"traceparent", "tracestate", "baggage"}, otel.GetTextMapPropagator().Fields())
	require.NoError(t, tel.Shutdown(t.Context()))
}

func TestSetupEnabledInstallsSDKProviders(t *testing.T) {
	keepGlobals(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	tel, err := Setup(t.Context(), config.Telemetry{Enabled: true}, "v1")
	require.NoError(t, err)

	assert.IsType(t, &sdktrace.TracerProvider{}, otel.GetTracerProvider())
	assert.IsType(t, &sdklog.LoggerProvider{}, tel.LoggerProvider)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, tel.Shutdown(ctx), "nothing was recorded, so shutdown has nothing to export")
}
