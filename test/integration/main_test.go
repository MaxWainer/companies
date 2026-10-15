//go:build integration

package integration

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"

	"example.com/companies/internal/app"
	"example.com/companies/internal/platform/config"
	"example.com/companies/internal/platform/telemetry"
)

const (
	topic     = "companies.events.it"
	jwtSecret = "integration-test-secret-0123456789abcdef"
)

var env struct {
	baseURL  string
	brokers  []string
	cfg      config.Config
	adminDSN string
}

func TestMain(m *testing.M) {
	flag.Parse()
	code, err := run(m)
	if err != nil {
		log.Printf("integration setup failed: %v", err)
		code = 1
	}
	os.Exit(code)
}

func run(m *testing.M) (code int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pg, err := tcpostgres.Run(ctx, "postgres:18.6-alpine",
		tcpostgres.WithDatabase("companies"),
		tcpostgres.WithUsername("companies"),
		tcpostgres.WithPassword("companies"),
		tcpostgres.BasicWaitStrategies(),
	)
	defer func() { err = errors.Join(err, terminate(pg)) }()
	if err != nil {
		return 0, fmt.Errorf("start postgres: %w", err)
	}
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return 0, err
	}
	env.adminDSN = dsn

	if _, err := telemetry.Setup(ctx, config.Telemetry{}, "test"); err != nil {
		return 0, err
	}

	rp, err := redpanda.Run(ctx, "redpandadata/redpanda:v26.2.4")
	defer func() { err = errors.Join(err, terminate(rp)) }()
	if err != nil {
		return 0, fmt.Errorf("start redpanda: %w", err)
	}
	broker, err := rp.KafkaSeedBroker(ctx)
	if err != nil {
		return 0, err
	}
	if err := createSinglePartitionTopics(ctx, broker, topic, topic+".dlq"); err != nil {
		return 0, err
	}

	cfg, err := config.FromMap(map[string]string{
		"COMPANIES_DATABASE_DSN":             dsn,
		"COMPANIES_KAFKA_BROKERS":            broker,
		"COMPANIES_KAFKA_TOPIC":              topic,
		"COMPANIES_KAFKA_CREATE_TOPIC":       "true",
		"COMPANIES_AUTH_JWT_SECRET":          jwtSecret,
		"COMPANIES_AUTH_ISSUER":              "integration-tests",
		"COMPANIES_AUTH_AUDIENCE":            "companies-service",
		"COMPANIES_HTTP_TRUST_TRACE_CONTEXT": "true",
	})
	if err != nil {
		return 0, err
	}
	logger := slog.New(slog.DiscardHandler)
	if testing.Verbose() {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	a, err := app.New(ctx, cfg, logger)
	if err != nil {
		return 0, fmt.Errorf("start app: %w", err)
	}
	defer a.Close()

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	runCtx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(runCtx, ln) }()

	env.baseURL = "http://" + ln.Addr().String()
	env.brokers = []string{broker}
	env.cfg = cfg

	code = m.Run()

	stop()
	if runErr := <-done; runErr != nil {
		return code, fmt.Errorf("app stopped with error: %w", runErr)
	}
	return code, nil
}

func terminate(c testcontainers.Container) error {
	if err := testcontainers.TerminateContainer(c); err != nil {
		return fmt.Errorf("terminate container: %w", err)
	}
	return nil
}

func createSinglePartitionTopics(ctx context.Context, broker string, topics ...string) error {
	client := &kafkago.Client{Addr: kafkago.TCP(broker), Timeout: 15 * time.Second}
	req := &kafkago.CreateTopicsRequest{}
	for _, t := range topics {
		req.Topics = append(req.Topics, kafkago.TopicConfig{Topic: t, NumPartitions: 1, ReplicationFactor: 1})
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
