package kafka

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	eventmodel "example.com/companies/internal/event/model"
)

type traced struct {
	next   EventPublisher
	topic  string
	tracer trace.Tracer
}

func Traced(next EventPublisher, topic string) EventPublisher {
	return traced{next: next, topic: topic, tracer: otel.Tracer("example.com/companies/internal/event/kafka")}
}

func (t traced) Publish(ctx context.Context, ev eventmodel.Event) error {
	ctx, span := t.tracer.Start(ctx, "send "+t.topic,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystemKafka,
			semconv.MessagingOperationTypeSend,
			semconv.MessagingDestinationName(t.topic),
			semconv.MessagingMessageID(ev.ID.String()),
			semconv.MessagingKafkaMessageKey(ev.Company.ID.String()),
			attribute.String("companies.event_type", string(ev.Type)),
		),
	)
	defer span.End()
	err := t.next.Publish(ctx, ev)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "not delivered")
	}
	return err
}
