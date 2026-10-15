package model

import (
	"time"

	"github.com/google/uuid"
)

type CreateParams struct {
	Name              string
	Description       *string
	AmountOfEmployees int
	Registered        bool
	Type              Type
}

func (p CreateParams) Validate() error {
	return NewCompany(uuid.Nil, p, time.Time{}).Validate()
}
