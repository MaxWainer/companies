package kafka

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	companymodel "example.com/companies/internal/company/model"
	"example.com/companies/internal/event/eventpb"
	eventmodel "example.com/companies/internal/event/model"
)

var (
	eventTypes = map[eventmodel.Type]eventpb.EventType{
		eventmodel.CompanyCreated: eventpb.EventType_EVENT_TYPE_COMPANY_CREATED,
		eventmodel.CompanyUpdated: eventpb.EventType_EVENT_TYPE_COMPANY_UPDATED,
		eventmodel.CompanyDeleted: eventpb.EventType_EVENT_TYPE_COMPANY_DELETED,
	}
	companyTypes = map[companymodel.Type]eventpb.CompanyType{
		companymodel.TypeCorporations:       eventpb.CompanyType_COMPANY_TYPE_CORPORATIONS,
		companymodel.TypeNonProfit:          eventpb.CompanyType_COMPANY_TYPE_NON_PROFIT,
		companymodel.TypeCooperative:        eventpb.CompanyType_COMPANY_TYPE_COOPERATIVE,
		companymodel.TypeSoleProprietorship: eventpb.CompanyType_COMPANY_TYPE_SOLE_PROPRIETORSHIP,
	}
)

func encode(ev eventmodel.Event) ([]byte, error) {
	return proto.Marshal(toProto(ev))
}

func toProto(ev eventmodel.Event) *eventpb.CompanyEvent {
	c := ev.Company
	return &eventpb.CompanyEvent{
		Id:            ev.ID.String(),
		Type:          eventTypes[ev.Type],
		OccurredAt:    timestamppb.New(ev.OccurredAt),
		Actor:         ev.Actor,
		ChangedFields: ev.ChangedFields,
		Company: &eventpb.Company{
			Id:                c.ID.String(),
			Name:              c.Name,
			Description:       c.Description,
			AmountOfEmployees: int32(c.AmountOfEmployees), //nolint:gosec // validated to fit int32 by the domain
			Registered:        c.Registered,
			Type:              companyTypes[c.Type],
			CreatedAt:         timestamppb.New(c.CreatedAt),
			UpdatedAt:         timestamppb.New(c.UpdatedAt),
		},
	}
}
