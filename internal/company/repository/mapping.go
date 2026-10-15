package repository

import (
	"example.com/companies/internal/company/model"
	"example.com/companies/internal/company/repository/companydb"
)

func toDomain(r companydb.Company) model.Company {
	return model.Company{
		ID:                r.ID,
		Name:              r.Name,
		Description:       r.Description,
		AmountOfEmployees: r.AmountOfEmployees,
		Registered:        r.Registered,
		Type:              model.Type(r.Type),
		CreatedAt:         r.CreatedAt.UTC(),
		UpdatedAt:         r.UpdatedAt.UTC(),
	}
}

func insertParams(c model.Company) companydb.InsertCompanyParams {
	return companydb.InsertCompanyParams{
		ID:                c.ID,
		Name:              c.Name,
		Description:       c.Description,
		AmountOfEmployees: c.AmountOfEmployees,
		Registered:        c.Registered,
		Type:              string(c.Type),
		CreatedAt:         c.CreatedAt,
		UpdatedAt:         c.UpdatedAt,
	}
}

func updateParams(c model.Company) companydb.UpdateCompanyParams {
	return companydb.UpdateCompanyParams{
		ID:                c.ID,
		Name:              c.Name,
		Description:       c.Description,
		AmountOfEmployees: c.AmountOfEmployees,
		Registered:        c.Registered,
		Type:              string(c.Type),
		UpdatedAt:         c.UpdatedAt,
	}
}
