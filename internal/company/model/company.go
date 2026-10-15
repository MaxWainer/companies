package model

import (
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
)

//go:generate go tool easyjson -disable_members_unescape company.go

const (
	MaxNameLength        = 15
	MaxDescriptionLength = 3000
	MaxEmployees         = math.MaxInt32
)

type Type string //	@name	CompanyType

const (
	TypeCorporations       Type = "CORPORATIONS"
	TypeNonProfit          Type = "NON_PROFIT"
	TypeCooperative        Type = "COOPERATIVE"
	TypeSoleProprietorship Type = "SOLE_PROPRIETORSHIP"
)

var types = []Type{TypeCorporations, TypeNonProfit, TypeCooperative, TypeSoleProprietorship}

func Types() []Type { return slices.Clone(types) }

func (t Type) Valid() bool { return slices.Contains(types, t) }

//easyjson:json
type Company struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	Description       *string   `json:"description"`
	AmountOfEmployees int       `json:"amount_of_employees"`
	Registered        bool      `json:"registered"`
	Type              Type      `json:"type"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
} //	@name	Company

func NewCompany(id uuid.UUID, p CreateParams, now time.Time) Company {
	return Company{
		ID:                id,
		Name:              normalizeName(p.Name),
		Description:       clonePtr(p.Description),
		AmountOfEmployees: p.AmountOfEmployees,
		Registered:        p.Registered,
		Type:              p.Type,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func (c Company) Validate() error {
	v := ValidationError{}
	v.check("name", validateName(c.Name))
	v.check("description", validateDescription(c.Description))
	v.check("amount_of_employees", validateEmployees(c.AmountOfEmployees))
	v.check("type", validateType(c.Type))
	return v.orNil()
}
