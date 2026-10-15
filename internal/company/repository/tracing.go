package repository

import (
	"context"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"example.com/companies/internal/company/model"
	"example.com/companies/internal/company/service"
	"example.com/companies/internal/platform/telemetry"
)

var attrCompanyID = attribute.Key("companies.company.id")

type traced struct {
	next   service.Repository
	tracer trace.Tracer
}

func Traced(next service.Repository) service.Repository {
	return traced{next: next, tracer: otel.Tracer("example.com/companies/internal/company/repository")}
}

func (t traced) InTx(ctx context.Context, fn func(ctx context.Context, tx service.Repository) error) (err error) {
	ctx, span := t.tracer.Start(ctx, "CompanyRepository.InTx")
	defer func() { end(span, err) }()
	return t.next.InTx(ctx, func(ctx context.Context, tx service.Repository) error {
		return fn(ctx, traced{next: tx, tracer: t.tracer})
	})
}

func (t traced) Create(ctx context.Context, c model.Company) (err error) {
	ctx, span := t.start(ctx, "CompanyRepository.Create", c.ID)
	defer func() { end(span, err) }()
	return t.next.Create(ctx, c)
}

func (t traced) Get(ctx context.Context, id uuid.UUID) (_ model.Company, err error) {
	ctx, span := t.start(ctx, "CompanyRepository.Get", id)
	defer func() { end(span, err) }()
	return t.next.Get(ctx, id)
}

func (t traced) GetForUpdate(ctx context.Context, id uuid.UUID) (_ model.Company, err error) {
	ctx, span := t.start(ctx, "CompanyRepository.GetForUpdate", id)
	defer func() { end(span, err) }()
	return t.next.GetForUpdate(ctx, id)
}

func (t traced) NameTaken(ctx context.Context, name string, except uuid.UUID) (_ bool, err error) {
	ctx, span := t.start(ctx, "CompanyRepository.NameTaken", except)
	defer func() { end(span, err) }()
	return t.next.NameTaken(ctx, name, except)
}

func (t traced) Update(ctx context.Context, c model.Company) (err error) {
	ctx, span := t.start(ctx, "CompanyRepository.Update", c.ID)
	defer func() { end(span, err) }()
	return t.next.Update(ctx, c)
}

func (t traced) Delete(ctx context.Context, id uuid.UUID) (_ model.Company, err error) {
	ctx, span := t.start(ctx, "CompanyRepository.Delete", id)
	defer func() { end(span, err) }()
	return t.next.Delete(ctx, id)
}

func (t traced) start(ctx context.Context, name string, id uuid.UUID) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, name, trace.WithAttributes(attrCompanyID.String(id.String())))
}

func end(span trace.Span, err error) { telemetry.EndSpan(span, err, model.Outcome) }
