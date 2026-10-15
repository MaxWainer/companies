//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	companymodel "example.com/companies/internal/company/model"
	companyrepo "example.com/companies/internal/company/repository"
	companyservice "example.com/companies/internal/company/service"
	eventmodel "example.com/companies/internal/event/model"
	"example.com/companies/internal/platform/config"
	"example.com/companies/internal/platform/postgres"
)

type recordingPublisher struct {
	mu     sync.Mutex
	events []eventmodel.Event
	err    error
}

func (p *recordingPublisher) Publish(_ context.Context, ev eventmodel.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, ev)
	return nil
}

func (p *recordingPublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}

type testDB struct {
	name   string
	pool   *pgxpool.Pool
	dsn    string
	events *recordingPublisher
	svc    *companyservice.Service
}

func newDatabase(t *testing.T) testDB {
	t.Helper()
	ctx := t.Context()

	b := make([]byte, 6)
	_, err := rand.Read(b)
	require.NoError(t, err)
	name := "it_" + hex.EncodeToString(b)

	admin, err := pgx.Connect(ctx, env.adminDSN)
	require.NoError(t, err)
	defer admin.Close(ctx)
	_, err = admin.Exec(ctx, "CREATE DATABASE "+name)
	require.NoError(t, err)

	u, err := url.Parse(env.adminDSN)
	require.NoError(t, err)
	u.Path = "/" + name
	pool, err := postgres.Connect(ctx, config.Database{DSN: u.String(), MaxConns: 4})
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Close()
		ctx := context.Background()
		admin, err := pgx.Connect(ctx, env.adminDSN)
		if err != nil {
			return
		}
		defer admin.Close(ctx)
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	_, err = postgres.Migrate(ctx, pool)
	require.NoError(t, err)
	events := &recordingPublisher{}
	return testDB{
		name:   name,
		pool:   pool,
		dsn:    u.String(),
		events: events,
		svc:    companyservice.New(companyrepo.New(pool), events),
	}
}

func (db testDB) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n))
	return n
}

func params(name string) companymodel.CreateParams {
	return companymodel.CreateParams{
		Name:              name,
		AmountOfEmployees: 3,
		Registered:        true,
		Type:              companymodel.TypeCooperative,
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	db := newDatabase(t)
	results, err := postgres.Migrate(t.Context(), db.pool)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestEveryWritePublishesOneEvent(t *testing.T) {
	db := newDatabase(t)
	ctx := t.Context()

	c, err := db.svc.Create(ctx, "alice", params("Evented"))
	require.NoError(t, err)
	_, err = db.svc.Patch(ctx, "alice", c.ID, companymodel.PatchParams{Registered: companymodel.Some(false)})
	require.NoError(t, err)
	_, err = db.svc.Patch(ctx, "alice", c.ID, companymodel.PatchParams{Registered: companymodel.Some(false)})
	require.NoError(t, err)
	require.NoError(t, db.svc.Delete(ctx, "alice", c.ID))

	types := make([]eventmodel.Type, 0, db.events.count())
	for _, ev := range db.events.events {
		types = append(types, ev.Type)
	}
	assert.Equal(t,
		[]eventmodel.Type{eventmodel.CompanyCreated, eventmodel.CompanyUpdated, eventmodel.CompanyDeleted}, types,
		"a patch that changes nothing publishes nothing")
}

func TestFailedPublishLeavesNoChange(t *testing.T) {
	db := newDatabase(t)
	ctx := t.Context()
	c, err := db.svc.Create(ctx, "alice", params("Kept"))
	require.NoError(t, err)

	db.events.err = eventmodel.ErrPublishFailed
	_, err = db.svc.Create(ctx, "alice", params("Lost"))
	require.ErrorIs(t, err, eventmodel.ErrPublishFailed)
	_, err = db.svc.Patch(ctx, "alice", c.ID, companymodel.PatchParams{Name: companymodel.Some("Renamed")})
	require.ErrorIs(t, err, eventmodel.ErrPublishFailed)
	require.ErrorIs(t, db.svc.Delete(ctx, "alice", c.ID), eventmodel.ErrPublishFailed)

	assert.Equal(t, 1, db.count(t, "companies"), "the failed create rolled back")
	got, err := db.svc.Get(ctx, c.ID)
	require.NoError(t, err, "the failed delete rolled back")
	assert.Equal(t, "Kept", got.Name, "the failed patch rolled back")
}

func TestRepositoryRollsBackFailedTransactions(t *testing.T) {
	db := newDatabase(t)
	ctx := t.Context()
	repo := companyrepo.New(db.pool)

	c, err := db.svc.Create(ctx, "alice", params("Rollback"))
	require.NoError(t, err)
	baseline := db.events.count()

	boom := errors.New("boom")
	err = repo.InTx(ctx, func(ctx context.Context, tx companyservice.Repository) error {
		changed := c
		changed.Name = "Changed"
		require.NoError(t, tx.Update(ctx, changed))
		return boom
	})
	require.ErrorIs(t, err, boom)
	got, err := repo.Get(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, c, got, "failed transaction is not persisted")

	require.ErrorIs(t, db.svc.Delete(ctx, "alice", uuid.New()), companymodel.ErrNotFound)
	_, err = db.svc.Patch(ctx, "alice", uuid.New(), companymodel.PatchParams{Registered: companymodel.Some(false)})
	require.ErrorIs(t, err, companymodel.ErrNotFound)
	_, err = db.svc.Create(ctx, "alice", params("ROLLBACK"))
	require.ErrorIs(t, err, companymodel.ErrNameTaken)
	assert.Equal(t, baseline, db.events.count(), "failed operations publish nothing")
}

func TestStoredValuesRoundTrip(t *testing.T) {
	db := newDatabase(t)
	ctx := t.Context()

	desc := "Многоязычное описание, with emoji 🚀 and \"quotes\""
	c, err := db.svc.Create(ctx, "alice", companymodel.CreateParams{
		Name:              "Ünïcødé Ltd",
		Description:       &desc,
		AmountOfEmployees: companymodel.MaxEmployees,
		Registered:        false,
		Type:              companymodel.TypeSoleProprietorship,
	})
	require.NoError(t, err)

	got, err := db.svc.Get(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, c, got, "what the API returns on create equals what is read back")
	assert.Equal(t, time.UTC, got.CreatedAt.Location())
}
