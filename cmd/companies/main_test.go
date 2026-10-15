package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpAndVersionNeedNoConfiguration(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		var out bytes.Buffer
		require.NoError(t, run(args, &out), args)
		assert.Contains(t, out.String(), "Usage: companies", args)
	}
	var out bytes.Buffer
	require.NoError(t, run([]string{"version"}, &out))
	assert.Equal(t, version+"\n", out.String())
}

func TestUsageErrorsAreReportedBeforeLoadingConfiguration(t *testing.T) {
	for _, args := range [][]string{
		{"bogus"},
		{"serve", "extra"},
		{"migrate", "extra"},
		{"healthcheck", "nope"},
		{"healthcheck", "ready", "live"},
	} {
		var ue usageError
		require.ErrorAs(t, run(args, &bytes.Buffer{}), &ue, args)
	}
}

func TestValidCommandsLoadConfiguration(t *testing.T) {
	t.Setenv("COMPANIES_DATABASE_DSN", "")
	err := run([]string{"healthcheck", "live"}, &bytes.Buffer{})
	require.ErrorContains(t, err, "COMPANIES_DATABASE_DSN is required")
}
