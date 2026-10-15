package model

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mailru/easyjson"
)

type plainCompany Company

func benchCompany() Company {
	desc := strings.Repeat("Lorem ipsum dolor sit amet. ", 20)
	now := time.Date(2026, 10, 14, 12, 0, 0, 123456000, time.UTC)
	return Company{
		ID: uuid.MustParse("01a13991-d356-7703-98ef-64f512705394"), Name: "Acme Holdings",
		Description: &desc, AmountOfEmployees: 4200, Registered: true,
		Type: TypeCorporations, CreatedAt: now, UpdatedAt: now,
	}
}

func TestBaselineMatchesGeneratedCodec(t *testing.T) {
	c := benchCompany()
	gen, err := easyjson.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	std, err := json.Marshal(plainCompany(c))
	if err != nil {
		t.Fatal(err)
	}
	if string(gen) != string(std) {
		t.Fatalf("easyjson and encoding/json disagree:\n%s\n%s", gen, std)
	}
}

func BenchmarkMarshalCompany(b *testing.B) {
	c := benchCompany()
	b.Run("easyjson", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := easyjson.Marshal(c); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("encoding_json", func(b *testing.B) {
		p := plainCompany(c)
		b.ReportAllocs()
		for b.Loop() {
			if _, err := json.Marshal(p); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkWriteCompany(b *testing.B) {
	c := benchCompany()
	b.Run("easyjson", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := easyjson.MarshalToWriter(c, io.Discard); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("encoding_json", func(b *testing.B) {
		p := plainCompany(c)
		b.ReportAllocs()
		for b.Loop() {
			if err := json.NewEncoder(io.Discard).Encode(p); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkValidate(b *testing.B) {
	c := benchCompany()
	b.ReportAllocs()
	for b.Loop() {
		if err := c.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPatchApply(b *testing.B) {
	base := benchCompany()
	p := PatchParams{
		Name:              Some("Acme Group"),
		AmountOfEmployees: Some(5000),
		Description:       Some[*string](nil),
	}
	b.ReportAllocs()
	for b.Loop() {
		c := base
		if len(p.Apply(&c)) != 3 {
			b.Fatal("unexpected change set")
		}
	}
}
