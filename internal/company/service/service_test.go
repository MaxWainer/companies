package service

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"example.com/companies/internal/company/model"
	eventmodel "example.com/companies/internal/event/model"
)

type fakeRepo struct {
	companies map[uuid.UUID]model.Company
}

func newFakeRepo() *fakeRepo { return &fakeRepo{companies: map[uuid.UUID]model.Company{}} }

func (r *fakeRepo) InTx(ctx context.Context, fn func(ctx context.Context, tx Repository) error) error {
	snapshot := maps.Clone(r.companies)
	if err := fn(ctx, r); err != nil {
		r.companies = snapshot
		return err
	}
	return nil
}

func (r *fakeRepo) Create(_ context.Context, c model.Company) error {
	for _, existing := range r.companies {
		if strings.EqualFold(existing.Name, c.Name) {
			return model.ErrNameTaken
		}
	}
	r.companies[c.ID] = c
	return nil
}

func (r *fakeRepo) Get(_ context.Context, id uuid.UUID) (model.Company, error) {
	c, ok := r.companies[id]
	if !ok {
		return model.Company{}, model.ErrNotFound
	}
	return c, nil
}

func (r *fakeRepo) GetForUpdate(ctx context.Context, id uuid.UUID) (model.Company, error) {
	return r.Get(ctx, id)
}

func (r *fakeRepo) NameTaken(_ context.Context, name string, except uuid.UUID) (bool, error) {
	for id, c := range r.companies {
		if id != except && strings.EqualFold(c.Name, name) {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepo) Update(_ context.Context, c model.Company) error {
	r.companies[c.ID] = c
	return nil
}

func (r *fakeRepo) Delete(_ context.Context, id uuid.UUID) (model.Company, error) {
	c, ok := r.companies[id]
	if !ok {
		return model.Company{}, model.ErrNotFound
	}
	delete(r.companies, id)
	return c, nil
}

type fakePublisher struct {
	events []eventmodel.Event
	err    error
}

func (p *fakePublisher) Publish(_ context.Context, ev eventmodel.Event) error {
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, ev)
	return nil
}

func newTestService() (*Service, *fakeRepo, *fakePublisher, *time.Time) {
	repo, pub := newFakeRepo(), &fakePublisher{}
	now := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
	svc := New(repo, pub)
	svc.now = func() time.Time { return now }
	return svc, repo, pub, &now
}

func TestServiceLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, _, pub, now := newTestService()

	created, err := svc.Create(ctx, "alice", model.CreateParams{
		Name: " Acme ", AmountOfEmployees: 5, Registered: true, Type: model.TypeNonProfit,
	})
	require.NoError(t, err)
	assert.Equal(t, "Acme", created.Name, "name is trimmed")
	assert.Equal(t, now.Truncate(time.Microsecond), created.CreatedAt)
	assert.Equal(t, created.CreatedAt, created.UpdatedAt)
	require.Len(t, pub.events, 1)
	assert.Equal(t, eventmodel.CompanyCreated, pub.events[0].Type)
	assert.Equal(t, "alice", pub.events[0].Actor)
	assert.Equal(t, created, pub.events[0].Company)
	assert.NotEqual(t, created.ID, pub.events[0].ID, "events get their own ids")

	*now = now.Add(time.Minute)
	patched, err := svc.Patch(ctx, "bob", created.ID, model.PatchParams{AmountOfEmployees: model.Some(6)})
	require.NoError(t, err)
	assert.Equal(t, 6, patched.AmountOfEmployees)
	assert.True(t, patched.UpdatedAt.After(patched.CreatedAt))
	require.Len(t, pub.events, 2)
	assert.Equal(t, eventmodel.CompanyUpdated, pub.events[1].Type)
	assert.Equal(t, []string{"amount_of_employees"}, pub.events[1].ChangedFields)

	same, err := svc.Patch(ctx, "bob", created.ID, model.PatchParams{AmountOfEmployees: model.Some(6)})
	require.NoError(t, err)
	assert.Equal(t, patched, same)
	assert.Len(t, pub.events, 2)

	require.NoError(t, svc.Delete(ctx, "carol", created.ID))
	require.Len(t, pub.events, 3)
	assert.Equal(t, eventmodel.CompanyDeleted, pub.events[2].Type)
	assert.Equal(t, patched, pub.events[2].Company)

	_, err = svc.Get(ctx, created.ID)
	require.ErrorIs(t, err, model.ErrNotFound)
}

func TestServiceCreateRejectsInvalid(t *testing.T) {
	svc, _, pub, _ := newTestService()
	_, err := svc.Create(context.Background(), "alice", model.CreateParams{Name: "   ", Type: "nope"})
	var ve *model.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "name")
	assert.Contains(t, ve.Fields, "type")
	assert.Empty(t, pub.events)
}

func TestServicePatchValidatesBeforeLookup(t *testing.T) {
	svc, _, _, _ := newTestService()
	_, err := svc.Patch(context.Background(), "alice", uuid.New(), model.PatchParams{AmountOfEmployees: model.Some(-1)})
	var ve *model.ValidationError
	require.ErrorAs(t, err, &ve, "invalid input wins over not-found")
	assert.NotErrorIs(t, err, model.ErrNotFound)
}

func TestServicePropagatesRepositoryErrors(t *testing.T) {
	ctx := context.Background()
	svc, _, pub, _ := newTestService()
	params := model.CreateParams{Name: "Acme", AmountOfEmployees: 1, Type: model.TypeCooperative}

	_, err := svc.Create(ctx, "alice", params)
	require.NoError(t, err)
	params.Name = "ACME"
	_, err = svc.Create(ctx, "alice", params)
	require.ErrorIs(t, err, model.ErrNameTaken)
	assert.Len(t, pub.events, 1, "failed create emits nothing")

	require.ErrorIs(t, svc.Delete(ctx, "alice", uuid.New()), model.ErrNotFound)
	_, err = svc.Patch(ctx, "alice", uuid.New(), model.PatchParams{Registered: model.Some(true)})
	require.ErrorIs(t, err, model.ErrNotFound)
}

func TestServicePatchRejectsInvalidResult(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := newTestService()
	c, err := svc.Create(
		ctx,
		"alice",
		model.CreateParams{Name: "Acme", AmountOfEmployees: 1, Type: model.TypeCooperative},
	)
	require.NoError(t, err)

	_, err = svc.Patch(ctx, "alice", c.ID, model.PatchParams{Name: model.Some("   ")})
	var ve *model.ValidationError
	require.ErrorAs(t, err, &ve)
	stored, _ := repo.Get(ctx, c.ID)
	assert.Equal(t, "Acme", stored.Name, "rejected patch leaves state untouched")
}

func TestCreateFailsWhenIDGenerationFails(t *testing.T) {
	svc := New(newFakeRepo(), &fakePublisher{})
	svc.newID = func() (uuid.UUID, error) { return uuid.Nil, errors.New("entropy exhausted") }
	_, err := svc.Create(context.Background(), "alice", model.CreateParams{
		Name: "Acme", AmountOfEmployees: 1, Type: model.TypeCooperative,
	})
	require.ErrorContains(t, err, "entropy exhausted")
}

func TestPatchRejectsATakenName(t *testing.T) {
	ctx := context.Background()
	svc, _, pub, _ := newTestService()
	_, err := svc.Create(
		ctx,
		"alice",
		model.CreateParams{Name: "Acme", AmountOfEmployees: 1, Type: model.TypeCooperative},
	)
	require.NoError(t, err)
	beta, err := svc.Create(
		ctx,
		"alice",
		model.CreateParams{Name: "Beta", AmountOfEmployees: 1, Type: model.TypeCooperative},
	)
	require.NoError(t, err)

	_, err = svc.Patch(ctx, "alice", beta.ID, model.PatchParams{Name: model.Some("ACME")})
	require.ErrorIs(t, err, model.ErrNameTaken)
	renamed, err := svc.Patch(ctx, "alice", beta.ID, model.PatchParams{Name: model.Some("BETA")})
	require.NoError(t, err, "changing only the case of its own name is allowed")
	assert.Equal(t, "BETA", renamed.Name)
	assert.Len(t, pub.events, 3)
}

func TestFailedPublishRollsTheWriteBack(t *testing.T) {
	ctx := context.Background()
	svc, repo, pub, _ := newTestService()
	c, err := svc.Create(
		ctx,
		"alice",
		model.CreateParams{Name: "Kept", AmountOfEmployees: 1, Type: model.TypeCooperative},
	)
	require.NoError(t, err)

	pub.err = eventmodel.ErrPublishFailed
	_, err = svc.Create(
		ctx,
		"alice",
		model.CreateParams{Name: "Lost", AmountOfEmployees: 1, Type: model.TypeCooperative},
	)
	require.ErrorIs(t, err, eventmodel.ErrPublishFailed)
	_, err = svc.Patch(ctx, "alice", c.ID, model.PatchParams{Name: model.Some("Renamed")})
	require.ErrorIs(t, err, eventmodel.ErrPublishFailed)
	require.ErrorIs(t, svc.Delete(ctx, "alice", c.ID), eventmodel.ErrPublishFailed)

	assert.Equal(t, map[uuid.UUID]model.Company{c.ID: c}, repo.companies, "every failed write rolled back")
}
