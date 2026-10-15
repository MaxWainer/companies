package kafka

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	eventmodel "example.com/companies/internal/event/model"
	"example.com/companies/internal/platform/config"
)

const (
	HeaderEventID     = "event_id"
	HeaderEventType   = "event_type"
	HeaderContentType = "content-type"

	HeaderDLQOriginalTopic = "dlq_original_topic"
	HeaderDLQReason        = "dlq_reason"
	HeaderDLQFailedAt      = "dlq_failed_at"

	contentTypeProtobuf = "application/x-protobuf"
	clientID            = "companies-service"
	writeTimeout        = 10 * time.Second
)

var poisonErrors = []kafkago.Error{
	kafkago.MessageSizeTooLarge,
	kafkago.RecordListTooLarge,
	kafkago.InvalidRecord,
	kafkago.InvalidTimestamp,
}

type EventPublisher interface {
	Publish(ctx context.Context, ev eventmodel.Event) error
}

type Producer struct {
	writer       *kafkago.Writer
	dlqWriter    *kafkago.Writer
	published    prometheus.Counter
	deadLettered prometheus.Counter
}

var _ EventPublisher = (*Producer)(nil)

func NewProducer(cfg config.Kafka, reg prometheus.Registerer) (*Producer, error) {
	sec, err := newSecurity(cfg)
	if err != nil {
		return nil, err
	}
	writer := func(topic string) *kafkago.Writer {
		return &kafkago.Writer{
			Addr:         kafkago.TCP(cfg.Brokers...),
			Topic:        topic,
			Balancer:     &kafkago.Murmur2Balancer{},
			RequiredAcks: kafkago.RequireAll,
			BatchSize:    1,
			WriteTimeout: writeTimeout,
			Transport:    sec.transport(),
		}
	}
	p := &Producer{
		writer:    writer(cfg.Topic),
		dlqWriter: writer(cfg.DeadLetterTopic),
		published: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "companies", Name: "events_published_total",
			Help: "Events published to the events topic.",
		}),
		deadLettered: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "companies", Name: "events_dead_lettered_total",
			Help: "Events the broker refused as poison and that went to the dead-letter topic instead.",
		}),
	}
	reg.MustRegister(p.published, p.deadLettered)
	return p, nil
}

func (p *Producer) Publish(ctx context.Context, ev eventmodel.Event) error {
	payload, err := encode(ev)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	msg := kafkago.Message{Key: []byte(ev.Company.ID.String()), Value: payload, Headers: headers(ctx, ev)}
	err = p.writer.WriteMessages(ctx, msg)
	switch {
	case err == nil:
		p.published.Inc()
		return nil
	case isPoison(err):
		return p.deadLetter(ctx, msg, err)
	default:
		return fmt.Errorf("%w: %w", eventmodel.ErrPublishFailed, err)
	}
}

func (p *Producer) deadLetter(ctx context.Context, msg kafkago.Message, reason error) error {
	msg.Headers = append(msg.Headers,
		kafkago.Header{Key: HeaderDLQOriginalTopic, Value: []byte(p.writer.Topic)},
		kafkago.Header{Key: HeaderDLQReason, Value: []byte(reason.Error())},
		kafkago.Header{Key: HeaderDLQFailedAt, Value: []byte(time.Now().UTC().Format(time.RFC3339Nano))},
	)
	if err := p.dlqWriter.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("%w: dead-letter topic: %w", eventmodel.ErrPublishFailed, err)
	}
	p.deadLettered.Inc()
	return nil
}

func headers(ctx context.Context, ev eventmodel.Event) []kafkago.Header {
	h := []kafkago.Header{
		{Key: HeaderEventID, Value: []byte(ev.ID.String())},
		{Key: HeaderEventType, Value: []byte(ev.Type)},
		{Key: HeaderContentType, Value: []byte(contentTypeProtobuf)},
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	for k, v := range carrier {
		h = append(h, kafkago.Header{Key: k, Value: []byte(v)})
	}
	return h
}

func isPoison(err error) bool {
	if perMessage, ok := errors.AsType[kafkago.WriteErrors](err); ok && len(perMessage) == 1 {
		err = perMessage[0]
	}
	kerr, ok := errors.AsType[kafkago.Error](err)
	return ok && slices.Contains(poisonErrors, kerr)
}

func (p *Producer) Close() error {
	return errors.Join(p.writer.Close(), p.dlqWriter.Close())
}

func EnsureTopic(ctx context.Context, cfg config.Kafka) error {
	sec, err := newSecurity(cfg)
	if err != nil {
		return err
	}
	client := &kafkago.Client{
		Addr:      kafkago.TCP(cfg.Brokers...),
		Timeout:   15 * time.Second,
		Transport: sec.transport(),
	}
	topics := []string{cfg.Topic, cfg.DeadLetterTopic}
	req := &kafkago.CreateTopicsRequest{}
	for _, t := range topics {
		req.Topics = append(req.Topics, kafkago.TopicConfig{Topic: t, NumPartitions: 3, ReplicationFactor: 1})
	}
	resp, err := client.CreateTopics(ctx, req)
	if err != nil {
		return fmt.Errorf("create topics: %w", err)
	}
	for _, t := range topics {
		if err := resp.Errors[t]; err != nil && !errors.Is(err, kafkago.TopicAlreadyExists) {
			return fmt.Errorf("create topic %s: %w", t, err)
		}
	}
	return nil
}
