package model

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/mailru/easyjson/jlexer"

	companymodel "example.com/companies/internal/company/model"
)

const benchCreateBody = `{"name":"Acme Holdings","description":"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",` +
	`"amount_of_employees":4200,"registered":true,"type":"CORPORATIONS"}`

type stdCreateRequest struct {
	Name              *string            `json:"name"`
	Description       *string            `json:"description"`
	AmountOfEmployees *int               `json:"amount_of_employees"`
	Registered        *bool              `json:"registered"`
	Type              *companymodel.Type `json:"type"`
}

func BenchmarkDecodeCreateRequest(b *testing.B) {
	data := []byte(benchCreateBody)
	b.Run("easyjson", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var req CreateRequest
			in := jlexer.Lexer{Data: data}
			req.UnmarshalEasyJSON(&in)
			in.Consumed()
			if in.Error() != nil {
				b.Fatal(in.Error())
			}
		}
	})
	b.Run("encoding_json", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var req stdCreateRequest
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil {
				b.Fatal(err)
			}
		}
	})
}
