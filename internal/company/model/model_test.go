package model

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:fix inline

func validCompany() Company {
	return Company{
		ID:                uuid.New(),
		Name:              "Acme",
		Description:       new("Makes everything"),
		AmountOfEmployees: 10,
		Registered:        true,
		Type:              TypeCorporations,
	}
}

func TestCompanyValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Company)
		field  string
	}{
		{"valid", func(*Company) {}, ""},
		{"name at limit", func(c *Company) { c.Name = strings.Repeat("a", MaxNameLength) }, ""},
		{"multibyte name at limit", func(c *Company) { c.Name = strings.Repeat("я", MaxNameLength) }, ""},
		{"empty name", func(c *Company) { c.Name = "" }, "name"},
		{"name too long", func(c *Company) { c.Name = strings.Repeat("a", MaxNameLength+1) }, "name"},
		{"control char in name", func(c *Company) { c.Name = "Ac\x00me" }, "name"},
		{"nil description", func(c *Company) { c.Description = nil }, ""},
		{
			"description at limit",
			func(c *Company) { c.Description = new(strings.Repeat("d", MaxDescriptionLength)) },
			"",
		},
		{
			"description too long",
			func(c *Company) { c.Description = new(strings.Repeat("d", MaxDescriptionLength+1)) },
			"description",
		},
		{"NUL in description", func(c *Company) { c.Description = new("a\x00b") }, "description"},
		{"zero employees", func(c *Company) { c.AmountOfEmployees = 0 }, ""},
		{"negative employees", func(c *Company) { c.AmountOfEmployees = -1 }, "amount_of_employees"},
		{"too many employees", func(c *Company) { c.AmountOfEmployees = MaxEmployees + 1 }, "amount_of_employees"},
		{"unknown type", func(c *Company) { c.Type = "LLC" }, "type"},
		{"type is case sensitive", func(c *Company) { c.Type = "non_profit" }, "type"},
		{"sole proprietorship", func(c *Company) { c.Type = TypeSoleProprietorship }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validCompany()
			tt.mutate(&c)
			err := c.Validate()
			if tt.field == "" {
				require.NoError(t, err)
				return
			}
			var ve *ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Contains(t, ve.Fields, tt.field)
			assert.Len(t, ve.Fields, 1)
		})
	}
}

func TestPatchApply(t *testing.T) {
	c := validCompany()
	changed := PatchParams{
		Name:              Some("  Acme  "),
		Description:       Some[*string](nil),
		AmountOfEmployees: Some(10),
		Registered:        Some(false),
		Type:              Some(TypeCooperative),
	}.Apply(&c)

	assert.Equal(t, []string{"description", "registered", "type"}, changed)
	assert.Equal(t, "Acme", c.Name)
	assert.Nil(t, c.Description)
	assert.False(t, c.Registered)
	assert.Equal(t, TypeCooperative, c.Type)
}

func TestPatchValidate(t *testing.T) {
	err := PatchParams{Name: Some(""), Type: Some(Type("bogus"))}.Validate()
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, []string{"name", "type"}, slices.Sorted(maps.Keys(ve.Fields)))

	require.NoError(t, PatchParams{}.Validate())
}

func TestTypes(t *testing.T) {
	got := Types()
	assert.Len(t, got, 4)
	got[0] = "mutated"
	assert.Equal(t, TypeCorporations, Types()[0], "Types returns a copy")
}

func TestValidationErrorMessageIsDeterministic(t *testing.T) {
	err := &ValidationError{Fields: map[string]string{"type": "bad", "name": "empty"}}
	assert.Equal(t, "invalid company: name: empty; type: bad", err.Error())
}
