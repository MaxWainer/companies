package kafka

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"example.com/companies/internal/platform/config"
	"example.com/companies/internal/platform/health"
)

func HealthCheck(cfg config.Kafka) (health.Check, error) {
	sec, err := newSecurity(cfg)
	if err != nil {
		return health.Check{}, err
	}
	dialer := sec.dialer()
	dialer.Timeout = 2 * time.Second
	return health.Check{
		Name: "kafka",
		Run: func(ctx context.Context) (map[string]string, error) {
			var errs []error
			for _, broker := range cfg.Brokers {
				details, err := probeBroker(ctx, dialer, broker, cfg.Topic, cfg.DeadLetterTopic)
				if err == nil {
					return details, nil
				}
				errs = append(errs, fmt.Errorf("%s: %w", broker, err))
			}
			return nil, errors.Join(errs...)
		},
	}, nil
}

func probeBroker(
	ctx context.Context,
	dialer *kafkago.Dialer,
	broker string,
	topics ...string,
) (map[string]string, error) {
	conn, err := dialer.DialContext(ctx, "tcp", broker)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("set deadline: %w", err)
		}
	}

	brokers, err := conn.Brokers()
	if err != nil {
		return nil, fmt.Errorf("list brokers: %w", err)
	}
	partitions, err := conn.ReadPartitions(topics...)
	if err != nil {
		return nil, fmt.Errorf("read partitions: %w", err)
	}
	details := map[string]string{"broker": broker, "brokers": strconv.Itoa(len(brokers))}
	count := map[string]int{}
	for i := range partitions {
		count[partitions[i].Topic]++
	}
	for _, t := range topics {
		if count[t] == 0 {
			return details, fmt.Errorf("topic %s not found", t)
		}
		details["partitions."+t] = strconv.Itoa(count[t])
	}
	return details, nil
}
