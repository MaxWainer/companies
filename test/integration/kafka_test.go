//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	kafkago "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	companymodel "example.com/companies/internal/company/model"
	"example.com/companies/internal/event/kafka"
	eventmodel "example.com/companies/internal/event/model"
)

func event(desc string) eventmodel.Event {
	now := time.Now().UTC()
	return eventmodel.Event{
		ID: uuid.New(), Type: eventmodel.CompanyCreated, OccurredAt: now, Actor: "alice",
		Company: companymodel.Company{
			ID: uuid.New(), Name: "Acme", Description: &desc, AmountOfEmployees: 1,
			Type: companymodel.TypeCooperative, CreatedAt: now, UpdatedAt: now,
		},
	}
}

func requestContext(t *testing.T, traceID string) context.Context {
	t.Helper()
	prop := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(prop) })
	tid, err := trace.TraceIDFromHex(traceID)
	require.NoError(t, err)
	sid, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)
	return trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled,
	}))
}

func waitUntilWritable(t *testing.T, pub *kafka.Producer) {
	t.Helper()
	require.Eventually(t, func() bool {
		err := pub.Publish(t.Context(), event("warm-up"))
		if err != nil {
			t.Log(err)
		}
		return err == nil
	}, 30*time.Second, 200*time.Millisecond, "a freshly created topic accepts writes")
}

func TestPublishCarriesKeyHeadersAndTraceContext(t *testing.T) {
	cfg := env.cfg.Kafka
	cfg.Topic = "companies.events.headers." + uuid.NewString()[:8]
	cfg.DeadLetterTopic = cfg.Topic + ".dlq"
	require.NoError(t, createSinglePartitionTopics(t.Context(), env.brokers[0], cfg.Topic, cfg.DeadLetterTopic))
	pub, err := kafka.NewProducer(cfg, prometheus.NewRegistry())
	require.NoError(t, err)
	defer pub.Close()

	waitUntilWritable(t, pub)

	const traceID = "11111111111111111111111111111111"
	ev := event("plain")
	require.NoError(t, pub.Publish(requestContext(t, traceID), ev))

	msgs := readTopic(t, cfg.Topic, 2)
	msg := msgs[len(msgs)-1]
	assert.Equal(t, ev.Company.ID.String(), string(msg.Key))
	assert.Equal(t, ev.ID.String(), header(msg, kafka.HeaderEventID))
	assert.Equal(t, string(eventmodel.CompanyCreated), header(msg, kafka.HeaderEventType))
	assert.Equal(t, "application/x-protobuf", header(msg, kafka.HeaderContentType))
	assert.Contains(t, header(msg, "traceparent"), traceID, "consumers continue the request's trace")
	assert.Equal(t, ev.ID.String(), decodeEvent(t, msg.Value).GetId())
}

func TestPoisonEventGoesToDeadLetterTopicAndTheWriteSucceeds(t *testing.T) {
	cfg := env.cfg.Kafka
	cfg.Topic = "companies.events.tiny." + uuid.NewString()[:8]
	cfg.DeadLetterTopic = cfg.Topic + ".dlq"
	require.NoError(t, createSinglePartitionTopics(t.Context(), env.brokers[0], cfg.DeadLetterTopic))
	client := &kafkago.Client{Addr: kafkago.TCP(env.brokers...), Timeout: 15 * time.Second}
	resp, err := client.CreateTopics(t.Context(), &kafkago.CreateTopicsRequest{Topics: []kafkago.TopicConfig{{
		Topic: cfg.Topic, NumPartitions: 1, ReplicationFactor: 1,
		ConfigEntries: []kafkago.ConfigEntry{{ConfigName: "max.message.bytes", ConfigValue: "2048"}},
	}}})
	require.NoError(t, err)
	require.NoError(t, resp.Errors[cfg.Topic])
	pub, err := kafka.NewProducer(cfg, prometheus.NewRegistry())
	require.NoError(t, err)
	defer pub.Close()

	waitUntilWritable(t, pub)

	const traceID = "dddddddddddddddddddddddddddddddd"
	poison := event(strings.Repeat("x", companymodel.MaxDescriptionLength))
	require.NoError(t, pub.Publish(requestContext(t, traceID), poison), "a poison event does not fail the write")

	dead := readTopic(t, cfg.DeadLetterTopic, 1)[0]
	assert.Equal(t, poison.Company.ID.String(), string(dead.Key))
	assert.Equal(t, cfg.Topic, header(dead, kafka.HeaderDLQOriginalTopic))
	assert.Contains(t, header(dead, kafka.HeaderDLQReason), "Message Size Too Large")
	assert.NotEmpty(t, header(dead, kafka.HeaderDLQFailedAt))
	assert.Contains(t, header(dead, "traceparent"), traceID, "the dead letter continues the request's trace")
	assert.Equal(t, poison.ID.String(), decodeEvent(t, dead.Value).GetId(), "the payload is kept unchanged")
}

func TestUnreachableBrokerFailsThePublish(t *testing.T) {
	cfg := env.cfg.Kafka
	cfg.Brokers = []string{"127.0.0.1:1"}
	pub, err := kafka.NewProducer(cfg, prometheus.NewRegistry())
	require.NoError(t, err)
	defer pub.Close()

	err = pub.Publish(t.Context(), event("x"))
	require.ErrorIs(t, err, eventmodel.ErrPublishFailed)
}

func readTopic(t *testing.T, topic string, n int) []kafkago.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	r := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers: env.brokers, Topic: topic, GroupID: "it-" + topic, StartOffset: kafkago.FirstOffset,
		MaxWait: 200 * time.Millisecond,
	})
	defer r.Close()
	var out []kafkago.Message
	for len(out) < n {
		msg, err := r.ReadMessage(ctx)
		require.NoError(t, err, "timed out reading %s", topic)
		out = append(out, msg)
	}
	return out
}

func TestKafkaHealthCheck(t *testing.T) {
	chk, err := kafka.HealthCheck(env.cfg.Kafka)
	require.NoError(t, err)
	details, err := chk.Run(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "1", details["partitions."+topic])
	assert.False(t, chk.Critical, "Kafka never takes the service out of rotation")

	missing := env.cfg.Kafka
	missing.DeadLetterTopic = "no-such-topic"
	missingChk, err := kafka.HealthCheck(missing)
	require.NoError(t, err)
	_, err = missingChk.Run(t.Context())
	require.Error(t, err)

	down := env.cfg.Kafka
	down.Brokers = []string{"127.0.0.1:1"}
	downChk, err := kafka.HealthCheck(down)
	require.NoError(t, err)
	_, err = downChk.Run(t.Context())
	require.ErrorContains(t, err, "dial")
}
