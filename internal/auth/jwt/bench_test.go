package jwt

import (
	"testing"
	"time"
)

func BenchmarkVerify(b *testing.B) {
	token, err := Issue(testCfg, "alice", time.Hour, time.Now())
	if err != nil {
		b.Fatal(err)
	}
	v := NewVerifier(testCfg)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := v.Verify(token); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkVerifyRejectsForgery(b *testing.B) {
	forged := testCfg
	forged.JWTSecret = "another-secret-another-secret-12345"
	token, err := Issue(forged, "mallory", time.Hour, time.Now())
	if err != nil {
		b.Fatal(err)
	}
	v := NewVerifier(testCfg)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := v.Verify(token); err == nil {
			b.Fatal("forged token accepted")
		}
	}
}
