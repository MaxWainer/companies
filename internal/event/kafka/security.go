package kafka

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"

	"example.com/companies/internal/platform/config"
)

type security struct {
	tls  *tls.Config
	sasl sasl.Mechanism
}

func newSecurity(cfg config.Kafka) (security, error) {
	var s security
	if cfg.TLSEnabled {
		tc, err := tlsConfig(cfg.TLSCAFile)
		if err != nil {
			return security{}, err
		}
		s.tls = tc
	}
	switch cfg.SASLMechanism {
	case "":
	case config.SASLPlain:
		s.sasl = plain.Mechanism{Username: cfg.SASLUsername, Password: cfg.SASLPassword}
	case config.SASLScramSHA256, config.SASLScramSHA512:
		algo := scram.SHA256
		if cfg.SASLMechanism == config.SASLScramSHA512 {
			algo = scram.SHA512
		}
		m, err := scram.Mechanism(algo, cfg.SASLUsername, cfg.SASLPassword)
		if err != nil {
			return security{}, fmt.Errorf("kafka sasl: %w", err)
		}
		s.sasl = m
	}
	return s, nil
}

func tlsConfig(caFile string) (*tls.Config, error) {
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile == "" {
		return tc, nil
	}
	pem, err := os.ReadFile(caFile) //nolint:gosec // path comes from operator configuration
	if err != nil {
		return nil, fmt.Errorf("kafka tls: read CA file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("kafka tls: no certificates in %s", caFile)
	}
	tc.RootCAs = pool
	return tc, nil
}

func (s security) transport() *kafkago.Transport {
	return &kafkago.Transport{ClientID: clientID, TLS: s.tls, SASL: s.sasl}
}

func (s security) dialer() *kafkago.Dialer {
	return &kafkago.Dialer{ClientID: clientID, TLS: s.tls, SASLMechanism: s.sasl, DualStack: true}
}
