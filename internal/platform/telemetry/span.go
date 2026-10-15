package telemetry

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func EndSpan(span trace.Span, err error, outcome func(error) string) {
	if err != nil {
		if o := outcome(err); o != "" {
			span.SetAttributes(attribute.String("error.type", o))
		} else {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
	}
	span.End()
}
