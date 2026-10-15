package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"example.com/companies/internal/company/model"
	eventmodel "example.com/companies/internal/event/model"
)

//go:generate go tool mockgen -typed -destination=../../mocks/company_service.go -package=mocks . Companies

type Companies interface {
	Create(ctx context.Context, actor string, p model.CreateParams) (model.Company, error)
	Get(ctx context.Context, id uuid.UUID) (model.Company, error)
	Patch(ctx context.Context, actor string, id uuid.UUID, p model.PatchParams) (model.Company, error)
	Delete(ctx context.Context, actor string, id uuid.UUID) error
}

type Repository interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx Repository) error) error
	Create(ctx context.Context, c model.Company) error
	Get(ctx context.Context, id uuid.UUID) (model.Company, error)
	GetForUpdate(ctx context.Context, id uuid.UUID) (model.Company, error)
	NameTaken(ctx context.Context, name string, except uuid.UUID) (bool, error)
	Update(ctx context.Context, c model.Company) error
	Delete(ctx context.Context, id uuid.UUID) (model.Company, error)
}

type Publisher interface {
	Publish(ctx context.Context, ev eventmodel.Event) error
}

type Service struct {
	repo   Repository
	events Publisher
	now    func() time.Time
	newID  func() (uuid.UUID, error)
}

var _ Companies = (*Service)(nil)

func New(repo Repository, events Publisher) *Service {
	return &Service{repo: repo, events: events, now: time.Now, newID: uuid.NewV7}
}

func (s *Service) Create(ctx context.Context, actor string, p model.CreateParams) (model.Company, error) {
	id, err := s.newID()
	if err != nil {
		return model.Company{}, fmt.Errorf("generate company id: %w", err)
	}
	c := model.NewCompany(id, p, s.timestamp())
	if err := c.Validate(); err != nil {
		return model.Company{}, err
	}
	ev, err := s.event(eventmodel.CompanyCreated, actor, c, nil)
	if err != nil {
		return model.Company{}, err
	}
	err = s.repo.InTx(ctx, func(ctx context.Context, tx Repository) error {
		if err := tx.Create(ctx, c); err != nil {
			return err
		}
		return s.events.Publish(ctx, ev)
	})
	if err != nil {
		return model.Company{}, err
	}
	return c, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (model.Company, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) Patch(ctx context.Context, actor string, id uuid.UUID, p model.PatchParams) (model.Company, error) {
	if err := p.Validate(); err != nil {
		return model.Company{}, err
	}
	var out model.Company
	err := s.repo.InTx(ctx, func(ctx context.Context, tx Repository) error {
		c, err := tx.GetForUpdate(ctx, id)
		if err != nil {
			return err
		}
		oldName := c.Name
		changed := p.Apply(&c)
		if len(changed) == 0 {
			out = c
			return nil
		}
		if err := c.Validate(); err != nil {
			return err
		}
		if c.Name != oldName {
			taken, err := tx.NameTaken(ctx, c.Name, c.ID)
			if err != nil {
				return err
			}
			if taken {
				return model.ErrNameTaken
			}
		}
		c.UpdatedAt = s.timestamp()
		if err := tx.Update(ctx, c); err != nil {
			return err
		}
		ev, err := s.event(eventmodel.CompanyUpdated, actor, c, changed)
		if err != nil {
			return err
		}
		out = c
		return s.events.Publish(ctx, ev)
	})
	if err != nil {
		return model.Company{}, err
	}
	return out, nil
}

func (s *Service) Delete(ctx context.Context, actor string, id uuid.UUID) error {
	return s.repo.InTx(ctx, func(ctx context.Context, tx Repository) error {
		c, err := tx.Delete(ctx, id)
		if err != nil {
			return err
		}
		ev, err := s.event(eventmodel.CompanyDeleted, actor, c, nil)
		if err != nil {
			return err
		}
		return s.events.Publish(ctx, ev)
	})
}

func (s *Service) event(t eventmodel.Type, actor string, c model.Company, changed []string) (eventmodel.Event, error) {
	id, err := s.newID()
	if err != nil {
		return eventmodel.Event{}, fmt.Errorf("generate event id: %w", err)
	}
	return eventmodel.Event{
		ID:            id,
		Type:          t,
		OccurredAt:    s.timestamp(),
		Actor:         actor,
		ChangedFields: changed,
		Company:       c,
	}, nil
}

func (s *Service) timestamp() time.Time {
	return s.now().UTC().Truncate(time.Microsecond)
}
