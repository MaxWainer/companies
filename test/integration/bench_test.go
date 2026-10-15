//go:build integration

package integration

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"example.com/companies/internal/auth/jwt"
)

var benchSeq atomic.Int64

func benchName() string {
	return "bn" + strconv.FormatInt(time.Now().UnixNano()%1e6, 36) + strconv.FormatInt(benchSeq.Add(1), 36)
}

var benchClient = &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 64}}

func benchDo(b *testing.B, method, path, token, body string, want int) []byte {
	b.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(b.Context(), method, env.baseURL+path, reader)
	if err != nil {
		b.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := benchClient.Do(req)
	if err != nil {
		b.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		b.Fatal(err)
	}
	if resp.StatusCode != want {
		b.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, data)
	}
	return data
}

func benchToken(b *testing.B) string {
	b.Helper()
	tok, err := jwt.Issue(env.cfg.Auth, "bench", time.Hour, time.Now())
	if err != nil {
		b.Fatal(err)
	}
	return tok
}

func createBody(name string) string {
	return fmt.Sprintf(`{"name":%q,"description":"benchmark company","amount_of_employees":10,`+
		`"registered":true,"type":"CORPORATIONS"}`, name)
}

func benchCreate(b *testing.B, tok string) string {
	b.Helper()
	data := benchDo(b, http.MethodPost, "/v1/companies", tok, createBody(benchName()), http.StatusCreated)
	i := strings.Index(string(data), `"id":"`) + len(`"id":"`)
	return string(data[i : i+36])
}

func BenchmarkAPI(b *testing.B) {
	tok := benchToken(b)

	b.Run("Create", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				benchCreate(b, tok)
			}
		})
	})

	b.Run("Get", func(b *testing.B) {
		path := "/v1/companies/" + benchCreate(b, tok)
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				benchDo(b, http.MethodGet, path, "", "", http.StatusOK)
			}
		})
	})

	b.Run("Patch", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			path := "/v1/companies/" + benchCreate(b, tok)
			n := 0
			for pb.Next() {
				n++
				benchDo(b, http.MethodPatch, path, tok, `{"amount_of_employees":`+strconv.Itoa(n)+`}`, http.StatusOK)
			}
		})
	})

	b.Run("Patch_same_row", func(b *testing.B) {
		path := "/v1/companies/" + benchCreate(b, tok)
		var n atomic.Int64
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				body := `{"amount_of_employees":` + strconv.FormatInt(n.Add(1), 10) + `}`
				benchDo(b, http.MethodPatch, path, tok, body, http.StatusOK)
			}
		})
	})

	b.Run("Create_Delete", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				id := benchCreate(b, tok)
				benchDo(b, http.MethodDelete, "/v1/companies/"+id, tok, "", http.StatusNoContent)
			}
		})
	})
}
