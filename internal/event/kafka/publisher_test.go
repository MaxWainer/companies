package kafka

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	eventmodel "example.com/companies/internal/event/model"
)

func TestIsPoison(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"message too large", kafkago.MessageSizeTooLarge, true},
		{"batch too large", kafkago.RecordListTooLarge, true},
		{"invalid record", kafkago.InvalidRecord, true},
		{"invalid timestamp", kafkago.InvalidTimestamp, true},
		{"wrapped poison", fmt.Errorf("write: %w", kafkago.MessageSizeTooLarge), true},
		{"per-message poison", kafkago.WriteErrors{kafkago.MessageSizeTooLarge}, true},
		{"not enough replicas", kafkago.NotEnoughReplicas, false},
		{"authorization", kafkago.TopicAuthorizationFailed, false},
		{"connection refused", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, false},
		{"connection reset", io.ErrUnexpectedEOF, false},
		{"deadline", context.DeadlineExceeded, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPoison(tt.err))
		})
	}
}

type stubPublisher struct{ err error }

func (s stubPublisher) Publish(context.Context, eventmodel.Event) error { return s.err }

func TestTracedPublisherRecordsAProducerSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(tracenoop.NewTracerProvider()) })

	ev := sampleEvent()
	require.NoError(t, Traced(stubPublisher{}, "topic").Publish(context.Background(), ev))
	require.ErrorIs(t, Traced(stubPublisher{err: io.EOF}, "topic").Publish(context.Background(), ev), io.EOF)

	spans := rec.Ended()
	require.Len(t, spans, 2)
	assert.Equal(t, "send topic", spans[0].Name())
	assert.Equal(t, trace.SpanKindProducer, spans[0].SpanKind())
	assert.Contains(t, spans[0].Attributes(), semconv.MessagingMessageID(ev.ID.String()))
	assert.Equal(t, codes.Unset, spans[0].Status().Code)
	assert.Equal(t, codes.Error, spans[1].Status().Code)
}
