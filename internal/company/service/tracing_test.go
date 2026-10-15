package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"example.com/companies/internal/company/model"
)

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(tracenoop.NewTracerProvider()) })
	return rec
}

func attrs(s sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
	m := map[attribute.Key]attribute.Value{}
	for _, kv := range s.Attributes() {
		m[kv.Key] = kv.Value
	}
	return m
}

func TestUseCasesAreTraced(t *testing.T) {
	rec := recordSpans(t)
	inner, _, _, _ := newTestService()
	svc := Traced(inner)
	ctx := context.Background()

	c, err := svc.Create(
		ctx,
		"alice",
		model.CreateParams{Name: "Acme", AmountOfEmployees: 1, Type: model.TypeCooperative},
	)
	require.NoError(t, err)
	_, err = svc.Patch(ctx, "bob", c.ID, model.PatchParams{Registered: model.Some(true)})
	require.NoError(t, err)

	spans := rec.Ended()
	require.Len(t, spans, 2)
	assert.Equal(t, "CompanyService.Create", spans[0].Name())
	assert.Equal(t, "alice", attrs(spans[0])["enduser.id"].AsString())
	assert.Equal(t, c.ID.String(), attrs(spans[0])["companies.company.id"].AsString())
	assert.Equal(t, "CompanyService.Patch", spans[1].Name())
	assert.Equal(t, c.ID.String(), attrs(spans[1])["companies.company.id"].AsString())
	for _, s := range spans {
		assert.Equal(t, codes.Unset, s.Status().Code)
	}
}

func TestDomainOutcomesAreNotSpanErrors(t *testing.T) {
	rec := recordSpans(t)
	inner, _, _, _ := newTestService()
	svc := Traced(inner)

	_, err := svc.Get(context.Background(), uuid.New())
	require.ErrorIs(t, err, model.ErrNotFound)

	span := rec.Ended()[0]
	assert.Equal(t, codes.Unset, span.Status().Code, "a 404 is an answer, not a failure")
	assert.Equal(t, "not_found", attrs(span)["error.type"].AsString())
}

type failingRepo struct{ fakeRepo }

func (failingRepo) InTx(context.Context, func(context.Context, Repository) error) error {
	return errors.New("connection reset")
}

func TestUnexpectedErrorsMarkTheSpan(t *testing.T) {
	rec := recordSpans(t)
	svc := Traced(New(&failingRepo{}, &fakePublisher{}))

	_, err := svc.Create(
		context.Background(),
		"alice",
		model.CreateParams{Name: "Acme", AmountOfEmployees: 1, Type: model.TypeCooperative},
	)
	require.Error(t, err)

	span := rec.Ended()[0]
	assert.Equal(t, codes.Error, span.Status().Code)
	require.Len(t, span.Events(), 1)
	assert.Equal(t, "exception", span.Events()[0].Name)
}
