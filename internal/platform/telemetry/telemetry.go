package telemetry

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"example.com/companies/internal/platform/config"
)

const ServiceName = "companies-service"

type Telemetry struct {
	LoggerProvider log.LoggerProvider
	shutdowns      []func(context.Context) error
}

func Setup(ctx context.Context, cfg config.Telemetry, version string) (*Telemetry, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	if !cfg.Enabled {
		return &Telemetry{}, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(ServiceName), semconv.ServiceVersion(version)),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithProcessRuntimeVersion(),
		resource.WithFromEnv(),
	)
	if err != nil && !errors.Is(err, resource.ErrPartialResource) {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExp), sdktrace.WithResource(res))

	logExp, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create log exporter: %w", err), tp.Shutdown(ctx))
	}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)), sdklog.WithResource(res))

	otel.SetTracerProvider(tp)
	otel.SetLoggerProvider(lp)
	return &Telemetry{
		LoggerProvider: lp,
		shutdowns:      []func(context.Context) error{tp.Shutdown, lp.Shutdown},
	}, nil
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs []error
	for _, fn := range t.shutdowns {
		errs = append(errs, fn(ctx))
	}
	return errors.Join(errs...)
}
