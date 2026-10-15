package config

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requiredVars() map[string]string {
	return map[string]string{
		"COMPANIES_DATABASE_DSN":    "postgres://u:p@db:5432/companies",
		"COMPANIES_KAFKA_BROKERS":   "k1:9092,k2:9092",
		"COMPANIES_AUTH_JWT_SECRET": "0123456789abcdef0123456789abcdef",
		"COMPANIES_AUTH_ISSUER":     "issuer",
		"COMPANIES_AUTH_AUDIENCE":   "companies",
	}
}

func TestFromMapDefaultsAndOverrides(t *testing.T) {
	vars := requiredVars()
	vars["COMPANIES_HTTP_ADDR"] = ":9000"
	vars["COMPANIES_TELEMETRY_ENABLED"] = "true"

	cfg, err := FromMap(vars)
	require.NoError(t, err)

	assert.Equal(t, ":9000", cfg.HTTP.Addr)
	assert.Equal(t, []string{"k1:9092", "k2:9092"}, cfg.Kafka.Brokers)
	assert.True(t, cfg.Telemetry.Enabled)
	assert.Equal(t, "companies.events.v1", cfg.Kafka.Topic)
	assert.True(t, cfg.Database.MigrateOnStart)
	assert.False(t, cfg.HTTP.DocsEnabled, "docs are off unless asked for")
	assert.Equal(t, "info", cfg.Log.Level)
}

func TestFromMapReportsAllProblems(t *testing.T) {
	_, err := FromMap(map[string]string{
		"COMPANIES_AUTH_JWT_SECRET": "short",
		"COMPANIES_LOG_FORMAT":      "xml",
	})
	require.Error(t, err)
	for _, want := range []string{
		"COMPANIES_DATABASE_DSN is required",
		"COMPANIES_KAFKA_BROKERS is required",
		"COMPANIES_AUTH_JWT_SECRET must be at least 32 bytes",
		"COMPANIES_LOG_FORMAT must be json or console",
	} {
		assert.ErrorContains(t, err, want)
	}
}

func TestFromMapRejectsMalformedValues(t *testing.T) {
	vars := requiredVars()
	vars["COMPANIES_HTTP_SHUTDOWN_TIMEOUT"] = "soon"
	_, err := FromMap(vars)
	assert.ErrorContains(t, err, `invalid duration "soon"`)
}

func TestLoadReadsProcessEnvironment(t *testing.T) {
	for k, v := range requiredVars() {
		t.Setenv(k, v)
	}
	t.Setenv("COMPANIES_LOG_LEVEL", "debug")
	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, []string{"k1:9092", "k2:9092"}, cfg.Kafka.Brokers)
}

func TestKafkaSecurityValidation(t *testing.T) {
	tests := map[string]struct {
		vars map[string]string
		want string
	}{
		"unknown mechanism": {map[string]string{"COMPANIES_KAFKA_SASL_MECHANISM": "GSSAPI"}, "must be one of PLAIN"},
		"missing credentials": {
			map[string]string{"COMPANIES_KAFKA_SASL_MECHANISM": "SCRAM-SHA-512", "COMPANIES_KAFKA_TLS_ENABLED": "true"},
			"COMPANIES_KAFKA_SASL_USERNAME and COMPANIES_KAFKA_SASL_PASSWORD are required",
		},
		"plain without tls": {
			map[string]string{
				"COMPANIES_KAFKA_SASL_MECHANISM": "PLAIN",
				"COMPANIES_KAFKA_SASL_USERNAME":  "u", "COMPANIES_KAFKA_SASL_PASSWORD": "p",
			},
			"sends the password in clear text",
		},
		"ca without tls": {
			map[string]string{"COMPANIES_KAFKA_TLS_CA_FILE": "/ca.pem"},
			"needs COMPANIES_KAFKA_TLS_ENABLED=true",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			vars := requiredVars()
			maps.Copy(vars, tt.vars)
			_, err := FromMap(vars)
			require.ErrorContains(t, err, tt.want)
		})
	}

	vars := requiredVars()
	maps.Copy(vars, map[string]string{
		"COMPANIES_KAFKA_SASL_MECHANISM": "SCRAM-SHA-256", "COMPANIES_KAFKA_TLS_ENABLED": "true",
		"COMPANIES_KAFKA_SASL_USERNAME": "u", "COMPANIES_KAFKA_SASL_PASSWORD": "p",
	})
	cfg, err := FromMap(vars)
	require.NoError(t, err)
	assert.Equal(t, "SCRAM-SHA-256", cfg.Kafka.SASLMechanism)
}
