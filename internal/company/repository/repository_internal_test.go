package repository

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"example.com/companies/internal/company/model"
)

func TestMapWriteError(t *testing.T) {
	taken := &pgconn.PgError{Code: uniqueViolationErr, ConstraintName: nameUniqueIndex}
	require.ErrorIs(t, mapWriteError(fmt.Errorf("exec: %w", taken), "update company"), model.ErrNameTaken,
		"a lost rename race still maps to 409")

	pk := &pgconn.PgError{Code: uniqueViolationErr, ConstraintName: "companies_pkey"}
	err := mapWriteError(pk, "insert company")
	require.NotErrorIs(t, err, model.ErrNameTaken, "only the name index means a taken name")
	require.ErrorContains(t, err, "insert company")
	require.ErrorIs(t, err, pk)

	boom := errors.New("boom")
	assert.ErrorIs(t, mapWriteError(boom, "update company"), boom)
}
