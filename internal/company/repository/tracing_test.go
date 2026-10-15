package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"example.com/companies/internal/company/model"
	"example.com/companies/internal/company/service"
)

type stubRepo struct {
	service.Repository
	err error
}

func (s stubRepo) InTx(ctx context.Context, fn func(context.Context, service.Repository) error) error {
	return fn(ctx, s)
}

func (s stubRepo) GetForUpdate(context.Context, uuid.UUID) (model.Company, error) {
	return model.Company{}, s.err
}

func (s stubRepo) Update(context.Context, model.Company) error { return s.err }

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(tracenoop.NewTracerProvider()) })
	return rec
}

func TestTracedWrapsTheTransactionalRepositoryToo(t *testing.T) {
	rec := recordSpans(t)
	repo := Traced(stubRepo{})
	id := uuid.New()

	err := repo.InTx(context.Background(), func(ctx context.Context, tx service.Repository) error {
		if _, err := tx.GetForUpdate(ctx, id); err != nil {
			return err
		}
		return tx.Update(ctx, model.Company{ID: id})
	})
	require.NoError(t, err)

	spans := rec.Ended()
	require.Len(t, spans, 3)
	txSpan := spans[2]
	assert.Equal(t, "CompanyRepository.InTx", txSpan.Name())
	for _, s := range spans[:2] {
		assert.Equal(
			t,
			txSpan.SpanContext().SpanID(),
			s.Parent().SpanID(),
			"%s runs inside the transaction span",
			s.Name(),
		)
	}
}

func TestTracedSeparatesDomainOutcomesFromFailures(t *testing.T) {
	rec := recordSpans(t)
	_, err := Traced(stubRepo{err: model.ErrNotFound}).GetForUpdate(context.Background(), uuid.New())
	require.ErrorIs(t, err, model.ErrNotFound)
	err = Traced(stubRepo{err: errors.New("connection reset")}).Update(context.Background(), model.Company{})
	require.Error(t, err)

	spans := rec.Ended()
	require.Len(t, spans, 2)
	assert.Equal(t, codes.Unset, spans[0].Status().Code, "not found is an answer, not a failure")
	assert.Equal(t, codes.Error, spans[1].Status().Code)
}
