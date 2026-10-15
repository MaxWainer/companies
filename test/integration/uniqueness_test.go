//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	companymodel "example.com/companies/internal/company/model"
	companyrepo "example.com/companies/internal/company/repository"
	companyservice "example.com/companies/internal/company/service"
	"example.com/companies/internal/platform/config"
	"example.com/companies/internal/platform/logging/logtest"
	"example.com/companies/internal/platform/postgres"
)

func TestTakenNameIsDetectedWithoutDatabaseErrors(t *testing.T) {
	db := newDatabase(t)
	ctx := t.Context()
	logger, logs := logtest.New()
	pool, err := postgres.Connect(ctx, config.Database{DSN: db.dsn, MaxConns: 2}, postgres.WithQueryLog(logger))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	svc := companyservice.New(companyrepo.New(pool), db.events)

	acme, err := svc.Create(ctx, "alice", params("Acme"))
	require.NoError(t, err)
	_, err = svc.Create(ctx, "alice", params("ACME"))
	require.ErrorIs(t, err, companymodel.ErrNameTaken, "insert conflict is case-insensitive")

	beta, err := svc.Create(ctx, "alice", params("Beta"))
	require.NoError(t, err)
	_, err = svc.Patch(ctx, "alice", beta.ID, companymodel.PatchParams{Name: companymodel.Some("acme")})
	require.ErrorIs(t, err, companymodel.ErrNameTaken, "rename onto a taken name")

	renamed, err := svc.Patch(ctx, "alice", beta.ID, companymodel.PatchParams{Name: companymodel.Some("BETA")})
	require.NoError(t, err, "changing only the case of its own name is allowed")
	assert.Equal(t, "BETA", renamed.Name)
	_, err = svc.Patch(ctx, "alice", acme.ID, companymodel.PatchParams{Registered: companymodel.Some(false)})
	require.NoError(t, err, "updates that keep the name skip the check")

	assert.Equal(t, 2, db.count(t, "companies"))
	assert.Equal(t, 4, db.events.count(), "rejected writes publish no events")
	for _, e := range logs.Entries() {
		assert.NotContains(t, []string{"query failed", "query rejected by constraint"}, e.Message,
			"a taken name must not surface as a database error: %v", e.Attrs)
	}
}
