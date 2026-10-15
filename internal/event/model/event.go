package model

import (
	"errors"
	"time"

	"github.com/google/uuid"

	companymodel "example.com/companies/internal/company/model"
)

type Type string

const (
	CompanyCreated Type = "company.created"
	CompanyUpdated Type = "company.updated"
	CompanyDeleted Type = "company.deleted"
)

var ErrPublishFailed = errors.New("event could not be published")

type Event struct {
	ID            uuid.UUID
	Type          Type
	OccurredAt    time.Time
	Actor         string
	ChangedFields []string
	Company       companymodel.Company
}
