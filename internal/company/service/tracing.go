package service

import (
	"context"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"example.com/companies/internal/company/model"
	"example.com/companies/internal/platform/telemetry"
)

var (
	attrCompanyID = attribute.Key("companies.company.id")
	attrActor     = attribute.Key("enduser.id")
)

type traced struct {
	next   Companies
	tracer trace.Tracer
}

func Traced(next Companies) Companies {
	return traced{next: next, tracer: otel.Tracer("example.com/companies/internal/company/service")}
}

func (t traced) Create(ctx context.Context, actor string, p model.CreateParams) (c model.Company, err error) {
	ctx, span := t.start(ctx, "CompanyService.Create", actor)
	defer func() {
		if err == nil {
			span.SetAttributes(attrCompanyID.String(c.ID.String()))
		}
		end(span, err)
	}()
	return t.next.Create(ctx, actor, p)
}

func (t traced) Get(ctx context.Context, id uuid.UUID) (_ model.Company, err error) {
	ctx, span := t.start(ctx, "CompanyService.Get", "", attrCompanyID.String(id.String()))
	defer func() { end(span, err) }()
	return t.next.Get(ctx, id)
}

func (t traced) Patch(
	ctx context.Context,
	actor string,
	id uuid.UUID,
	p model.PatchParams,
) (_ model.Company, err error) {
	ctx, span := t.start(ctx, "CompanyService.Patch", actor, attrCompanyID.String(id.String()))
	defer func() { end(span, err) }()
	return t.next.Patch(ctx, actor, id, p)
}

func (t traced) Delete(ctx context.Context, actor string, id uuid.UUID) (err error) {
	ctx, span := t.start(ctx, "CompanyService.Delete", actor, attrCompanyID.String(id.String()))
	defer func() { end(span, err) }()
	return t.next.Delete(ctx, actor, id)
}

func (t traced) start(
	ctx context.Context,
	name, actor string,
	attrs ...attribute.KeyValue,
) (context.Context, trace.Span) {
	if actor != "" {
		attrs = append(attrs, attrActor.String(actor))
	}
	return t.tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

func end(span trace.Span, err error) { telemetry.EndSpan(span, err, model.Outcome) }
