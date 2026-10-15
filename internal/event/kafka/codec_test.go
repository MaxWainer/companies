package kafka

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	companymodel "example.com/companies/internal/company/model"
	"example.com/companies/internal/event/eventpb"
	eventmodel "example.com/companies/internal/event/model"
)

func sampleEvent() eventmodel.Event {
	desc := strings.Repeat("Lorem ipsum dolor sit amet. ", 20)
	now := time.Date(2026, 10, 14, 12, 0, 0, 123456000, time.UTC)
	return eventmodel.Event{
		ID: uuid.MustParse("01a13991-d356-7729-9ced-e1a57848bde8"), Type: eventmodel.CompanyUpdated,
		OccurredAt: now.Add(time.Second), Actor: "alice",
		ChangedFields: []string{"description", "amount_of_employees"},
		Company: companymodel.Company{
			ID: uuid.MustParse("01a13991-d356-7703-98ef-64f512705394"), Name: "Acme Holdings",
			Description: &desc, AmountOfEmployees: 4200, Registered: true,
			Type: companymodel.TypeCorporations, CreatedAt: now, UpdatedAt: now,
		},
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	ev := sampleEvent()
	data, err := encode(ev)
	require.NoError(t, err)

	var got eventpb.CompanyEvent
	require.NoError(t, proto.Unmarshal(data, &got))
	assert.Equal(t, ev.ID.String(), got.GetId())
	assert.Equal(t, eventpb.EventType_EVENT_TYPE_COMPANY_UPDATED, got.GetType())
	assert.True(t, got.GetOccurredAt().AsTime().Equal(ev.OccurredAt))
	assert.Equal(t, "alice", got.GetActor())
	assert.Equal(t, ev.ChangedFields, got.GetChangedFields())
	c := got.GetCompany()
	assert.Equal(t, ev.Company.ID.String(), c.GetId())
	assert.Equal(t, *ev.Company.Description, c.GetDescription())
	assert.EqualValues(t, 4200, c.GetAmountOfEmployees())
	assert.Equal(t, eventpb.CompanyType_COMPANY_TYPE_CORPORATIONS, c.GetType())
	assert.True(t, c.GetCreatedAt().AsTime().Equal(ev.Company.CreatedAt))
}

func TestNilDescriptionStaysAbsent(t *testing.T) {
	ev := sampleEvent()
	ev.Company.Description = nil
	assert.Nil(t, toProto(ev).GetCompany().Description, "optional field distinguishes null from empty")
}

func TestEveryDomainTypeHasAProtoValue(t *testing.T) {
	for _, typ := range companymodel.Types() {
		ev := sampleEvent()
		ev.Company.Type = typ
		assert.NotEqual(t, eventpb.CompanyType_COMPANY_TYPE_UNSPECIFIED, toProto(ev).GetCompany().GetType(), typ)
	}
	for _, typ := range []eventmodel.Type{eventmodel.CompanyCreated, eventmodel.CompanyUpdated, eventmodel.CompanyDeleted} {
		ev := sampleEvent()
		ev.Type = typ
		assert.NotEqual(t, eventpb.EventType_EVENT_TYPE_UNSPECIFIED, toProto(ev).GetType(), typ)
	}
}

func BenchmarkEncode(b *testing.B) {
	ev := sampleEvent()
	b.Run("protobuf", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := encode(ev); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("json", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := json.Marshal(ev); err != nil {
				b.Fatal(err)
			}
		}
	})
}
