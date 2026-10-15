package model

import (
	"github.com/mailru/easyjson"
	"github.com/mailru/easyjson/jlexer"

	companymodel "example.com/companies/internal/company/model"
	"example.com/companies/internal/platform/httpserver/request"
)

var (
	_ easyjson.Unmarshaler = (*CreateRequest)(nil)
	_ easyjson.Unmarshaler = (*PatchRequest)(nil)
)

const notAllowed = "is not allowed"

type CreateRequest struct {
	Name              *string            `json:"name"                example:"Acme"             maxLength:"15"   minLength:"1"`
	Description       *string            `json:"description"         example:"Makes everything" maxLength:"3000"               extensions:"x-nullable"`
	AmountOfEmployees *int               `json:"amount_of_employees" example:"42"                                                                      maximum:"2147483647" minimum:"0"`
	Registered        *bool              `json:"registered"          example:"true"`
	Type              *companymodel.Type `json:"type"                example:"CORPORATIONS"                                                                                             enums:"CORPORATIONS,NON_PROFIT,COOPERATIVE,SOLE_PROPRIETORSHIP"`
} //	@name	CreateCompanyRequest

func (r *CreateRequest) UnmarshalEasyJSON(in *jlexer.Lexer) {
	request.DecodeObject(in, func(name string) {
		switch name {
		case "name":
			r.Name = request.ReadPtr(in, name, request.ReadString)
		case "description":
			r.Description = request.ReadPtr(in, name, request.ReadString)
		case "amount_of_employees":
			r.AmountOfEmployees = request.ReadPtr(in, name, request.ReadInt)
		case "registered":
			r.Registered = request.ReadPtr(in, name, request.ReadBool)
		case "type":
			r.Type = request.ReadPtr(in, name, readType)
		default:
			request.RejectField(in, name, notAllowed)
		}
	})
}

func (r *CreateRequest) Validate() error {
	missing := companymodel.ValidationError{Fields: map[string]string{}}
	require := func(ok bool, field string) {
		if !ok {
			missing.Fields[field] = "is required"
		}
	}
	require(r.Name != nil, "name")
	require(r.AmountOfEmployees != nil, "amount_of_employees")
	require(r.Registered != nil, "registered")
	require(r.Type != nil, "type")
	if len(missing.Fields) > 0 {
		return &missing
	}
	return r.Params().Validate()
}

func (r *CreateRequest) Params() companymodel.CreateParams {
	return companymodel.CreateParams{
		Name:              *r.Name,
		Description:       r.Description,
		AmountOfEmployees: *r.AmountOfEmployees,
		Registered:        *r.Registered,
		Type:              *r.Type,
	}
}

type PatchRequest struct {
	Name              request.Optional[string]            `json:"name"                swaggertype:"string"  example:"Acme Group" maxLength:"15"`
	Description       request.Optional[string]            `json:"description"         swaggertype:"string"                       maxLength:"3000" extensions:"x-nullable"`
	AmountOfEmployees request.Optional[int]               `json:"amount_of_employees" swaggertype:"integer" example:"50"                                                  minimum:"0"`
	Registered        request.Optional[bool]              `json:"registered"          swaggertype:"boolean"`
	Type              request.Optional[companymodel.Type] `json:"type"                swaggertype:"string"                                                                            enums:"CORPORATIONS,NON_PROFIT,COOPERATIVE,SOLE_PROPRIETORSHIP"`
} //	@name	PatchCompanyRequest

func (r *PatchRequest) UnmarshalEasyJSON(in *jlexer.Lexer) {
	request.DecodeObject(in, func(name string) {
		switch name {
		case "name":
			r.Name = request.ReadOptional(in, name, request.ReadString)
		case "description":
			r.Description = request.ReadOptional(in, name, request.ReadString)
		case "amount_of_employees":
			r.AmountOfEmployees = request.ReadOptional(in, name, request.ReadInt)
		case "registered":
			r.Registered = request.ReadOptional(in, name, request.ReadBool)
		case "type":
			r.Type = request.ReadOptional(in, name, readType)
		default:
			request.RejectField(in, name, notAllowed)
		}
	})
}

func (r *PatchRequest) Validate() error {
	notNull := companymodel.ValidationError{Fields: map[string]string{}}
	required := func(field string, null bool) {
		if null {
			notNull.Fields[field] = "must not be null"
		}
	}
	required("name", r.Name.Null)
	required("amount_of_employees", r.AmountOfEmployees.Null)
	required("registered", r.Registered.Null)
	required("type", r.Type.Null)
	if len(notNull.Fields) > 0 {
		return &notNull
	}
	return r.Params().Validate()
}

func (r *PatchRequest) Params() companymodel.PatchParams {
	var p companymodel.PatchParams
	if r.Name.Set {
		p.Name = companymodel.Some(r.Name.Value)
	}
	if r.Description.Set {
		var desc *string
		if !r.Description.Null {
			desc = &r.Description.Value
		}
		p.Description = companymodel.Some(desc)
	}
	if r.AmountOfEmployees.Set {
		p.AmountOfEmployees = companymodel.Some(r.AmountOfEmployees.Value)
	}
	if r.Registered.Set {
		p.Registered = companymodel.Some(r.Registered.Value)
	}
	if r.Type.Set {
		p.Type = companymodel.Some(r.Type.Value)
	}
	return p
}

func readType(in *jlexer.Lexer, field string) companymodel.Type {
	return companymodel.Type(request.ReadString(in, field))
}
