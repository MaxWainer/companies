package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/companies/internal/company/model"
	"example.com/companies/internal/company/repository/companydb"
	"example.com/companies/internal/company/service"
)

const (
	nameUniqueIndex    = "companies_name_key"
	uniqueViolationErr = "23505"
)

type Repository struct {
	pool *pgxpool.Pool
	q    *companydb.Queries
}

var _ service.Repository = (*Repository)(nil)

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, q: companydb.New(pool)}
}

func (r *Repository) InTx(ctx context.Context, fn func(ctx context.Context, tx service.Repository) error) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		return fn(ctx, &Repository{q: r.q.WithTx(tx)})
	})
}

func (r *Repository) Create(ctx context.Context, c model.Company) error {
	inserted, err := r.q.InsertCompany(ctx, insertParams(c))
	if err != nil {
		return mapWriteError(err, "insert company")
	}
	if inserted == 0 {
		return model.ErrNameTaken
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (model.Company, error) {
	row, err := r.q.GetCompany(ctx, id)
	if err != nil {
		return model.Company{}, mapReadError(err)
	}
	return toDomain(row), nil
}

func (r *Repository) GetForUpdate(ctx context.Context, id uuid.UUID) (model.Company, error) {
	row, err := r.q.GetCompanyForUpdate(ctx, id)
	if err != nil {
		return model.Company{}, mapReadError(err)
	}
	return toDomain(row), nil
}

func (r *Repository) NameTaken(ctx context.Context, name string, except uuid.UUID) (bool, error) {
	taken, err := r.q.NameTakenByOther(ctx, companydb.NameTakenByOtherParams{Name: name, ID: except})
	if err != nil {
		return false, fmt.Errorf("check name: %w", err)
	}
	return taken, nil
}

func (r *Repository) Update(ctx context.Context, c model.Company) error {
	if err := r.q.UpdateCompany(ctx, updateParams(c)); err != nil {
		return mapWriteError(err, "update company")
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) (model.Company, error) {
	row, err := r.q.DeleteCompany(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Company{}, model.ErrNotFound
		}
		return model.Company{}, fmt.Errorf("delete company: %w", err)
	}
	return toDomain(row), nil
}

func mapReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	}
	return fmt.Errorf("load company: %w", err)
}

func mapWriteError(err error, op string) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolationErr &&
		pgErr.ConstraintName == nameUniqueIndex {
		return model.ErrNameTaken
	}
	return fmt.Errorf("%s: %w", op, err)
}
