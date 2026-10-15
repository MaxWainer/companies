package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

const EnvPrefix = "COMPANIES_"

type Config struct {
	HTTP      HTTP      `envPrefix:"HTTP_"`
	Database  Database  `envPrefix:"DATABASE_"`
	Kafka     Kafka     `envPrefix:"KAFKA_"`
	Auth      Auth      `envPrefix:"AUTH_"`
	Log       Log       `envPrefix:"LOG_"`
	Telemetry Telemetry `envPrefix:"TELEMETRY_"`
}

type HTTP struct {
	Addr              string        `env:"ADDR"                envDefault:":8080"`
	ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT"    envDefault:"20s"`
	ShutdownDelay     time.Duration `env:"SHUTDOWN_DELAY"      envDefault:"0s"`
	DocsEnabled       bool          `env:"DOCS_ENABLED"        envDefault:"false"`
	PprofEnabled      bool          `env:"PPROF_ENABLED"       envDefault:"false"`
	TrustTraceContext bool          `env:"TRUST_TRACE_CONTEXT" envDefault:"false"`
}

type Database struct {
	DSN            string `env:"DSN"`
	MaxConns       int32  `env:"MAX_CONNS"        envDefault:"10"`
	MigrateOnStart bool   `env:"MIGRATE_ON_START" envDefault:"true"`
}

type Kafka struct {
	Brokers         []string `env:"BROKERS"`
	Topic           string   `env:"TOPIC"             envDefault:"companies.events.v1"`
	DeadLetterTopic string   `env:"DEAD_LETTER_TOPIC" envDefault:"companies.events.v1.dlq"`
	CreateTopic     bool     `env:"CREATE_TOPIC"      envDefault:"false"`

	TLSEnabled    bool   `env:"TLS_ENABLED"    envDefault:"false"`
	TLSCAFile     string `env:"TLS_CA_FILE"`
	SASLMechanism string `env:"SASL_MECHANISM"`
	SASLUsername  string `env:"SASL_USERNAME"`
	SASLPassword  string `env:"SASL_PASSWORD"`
}

const (
	SASLPlain       = "PLAIN"
	SASLScramSHA256 = "SCRAM-SHA-256"
	SASLScramSHA512 = "SCRAM-SHA-512"
)

type Auth struct {
	JWTSecret string `env:"JWT_SECRET"`
	Issuer    string `env:"ISSUER"`
	Audience  string `env:"AUDIENCE"`
}

type Log struct {
	Level  string `env:"LEVEL"  envDefault:"info"`
	Format string `env:"FORMAT" envDefault:"json"`
}

type Telemetry struct {
	Enabled bool `env:"ENABLED" envDefault:"false"`
}

const MinSecretLength = 32

func Load() (Config, error) {
	return FromMap(environ())
}

func FromMap(vars map[string]string) (Config, error) {
	var cfg Config
	if err := env.ParseWithOptions(&cfg, env.Options{Prefix: EnvPrefix, Environment: vars}); err != nil {
		return Config{}, fmt.Errorf("parse environment: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

func environ() map[string]string {
	vars := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}
	return vars
}

func (c Config) Validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	v := func(name string) string { return EnvPrefix + name }

	check(c.HTTP.Addr != "", "%s is required", v("HTTP_ADDR"))
	check(c.HTTP.ShutdownDelay >= 0, "%s must not be negative", v("HTTP_SHUTDOWN_DELAY"))
	check(c.Database.DSN != "", "%s is required", v("DATABASE_DSN"))
	check(c.Database.MaxConns > 0, "%s must be positive", v("DATABASE_MAX_CONNS"))
	check(len(c.Kafka.Brokers) > 0, "%s is required", v("KAFKA_BROKERS"))
	check(c.Kafka.Topic != "", "%s is required", v("KAFKA_TOPIC"))
	check(c.Kafka.DeadLetterTopic != "" && c.Kafka.DeadLetterTopic != c.Kafka.Topic,
		"%s is required and must differ from %s", v("KAFKA_DEAD_LETTER_TOPIC"), v("KAFKA_TOPIC"))
	switch c.Kafka.SASLMechanism {
	case "":
	case SASLPlain, SASLScramSHA256, SASLScramSHA512:
		check(
			c.Kafka.SASLUsername != "" && c.Kafka.SASLPassword != "",
			"%s and %s are required with %s",
			v("KAFKA_SASL_USERNAME"),
			v("KAFKA_SASL_PASSWORD"),
			v("KAFKA_SASL_MECHANISM"),
		)
		check(c.Kafka.SASLMechanism != SASLPlain || c.Kafka.TLSEnabled,
			"%s=PLAIN sends the password in clear text; set %s=true", v("KAFKA_SASL_MECHANISM"), v("KAFKA_TLS_ENABLED"))
	default:
		check(false, "%s must be one of PLAIN, SCRAM-SHA-256, SCRAM-SHA-512", v("KAFKA_SASL_MECHANISM"))
	}
	check(c.Kafka.TLSCAFile == "" || c.Kafka.TLSEnabled,
		"%s needs %s=true", v("KAFKA_TLS_CA_FILE"), v("KAFKA_TLS_ENABLED"))
	check(len(c.Auth.JWTSecret) >= MinSecretLength,
		"%s must be at least %d bytes", v("AUTH_JWT_SECRET"), MinSecretLength)
	check(c.Auth.Issuer != "", "%s is required", v("AUTH_ISSUER"))
	check(c.Auth.Audience != "", "%s is required", v("AUTH_AUDIENCE"))
	check(slices.Contains([]string{"debug", "info", "warn", "error"}, c.Log.Level),
		"%s must be one of debug, info, warn, error", v("LOG_LEVEL"))
	check(slices.Contains([]string{"json", "console"}, c.Log.Format), "%s must be json or console", v("LOG_FORMAT"))
	return errors.Join(errs...)
}
