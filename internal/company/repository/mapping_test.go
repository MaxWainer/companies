package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"example.com/companies/internal/company/model"
	"example.com/companies/internal/company/repository/companydb"
)

func TestRowMappingRoundTrip(t *testing.T) {
	desc := "d"
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.FixedZone("X", 3600))
	row := companydb.Company{
		ID: uuid.New(), Name: "Acme", Description: &desc, AmountOfEmployees: 3, Registered: true,
		Type: "NON_PROFIT", CreatedAt: at, UpdatedAt: at,
	}
	c := toDomain(row)
	assert.Equal(t, time.UTC, c.CreatedAt.Location(), "timestamps are normalized to UTC")
	assert.Equal(t, model.TypeNonProfit, c.Type)

	ins := insertParams(c)
	assert.Equal(t, row.ID, ins.ID)
	assert.Equal(t, row.Description, ins.Description)
	assert.True(t, ins.CreatedAt.Equal(at))

	upd := updateParams(c)
	assert.Equal(t, row.Name, upd.Name)
	assert.Equal(t, row.Type, upd.Type)
	assert.True(t, upd.UpdatedAt.Equal(at))
}
