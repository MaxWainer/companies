package jwt

import (
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"example.com/companies/internal/platform/config"
)

var testCfg = config.Auth{
	JWTSecret: "test-secret-test-secret-test-secret!",
	Issuer:    "https://issuer.example",
	Audience:  "companies",
}

func TestVerifyAcceptsIssuedToken(t *testing.T) {
	token, err := Issue(testCfg, "alice", time.Minute, time.Now())
	require.NoError(t, err)

	p, err := NewVerifier(testCfg).Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "alice", p.Subject)
}

func TestVerifyRejects(t *testing.T) {
	now := time.Now()
	sign := func(method gojwt.SigningMethod, key any, claims gojwt.RegisteredClaims) string {
		s, err := gojwt.NewWithClaims(method, claims).SignedString(key)
		require.NoError(t, err)
		return s
	}
	valid := gojwt.RegisteredClaims{
		Subject:   "alice",
		Issuer:    testCfg.Issuer,
		Audience:  gojwt.ClaimStrings{testCfg.Audience},
		IssuedAt:  gojwt.NewNumericDate(now),
		ExpiresAt: gojwt.NewNumericDate(now.Add(time.Minute)),
	}
	with := func(f func(*gojwt.RegisteredClaims)) gojwt.RegisteredClaims {
		c := valid
		f(&c)
		return c
	}
	secret := []byte(testCfg.JWTSecret)

	tests := map[string]string{
		"garbage":      "not.a.jwt",
		"wrong secret": sign(gojwt.SigningMethodHS256, []byte("another-secret-another-secret-12345"), valid),
		"alg none":     sign(gojwt.SigningMethodNone, gojwt.UnsafeAllowNoneSignatureType, valid),
		"alg HS512":    sign(gojwt.SigningMethodHS512, secret, valid),
		"expired": sign(
			gojwt.SigningMethodHS256,
			secret,
			with(func(c *gojwt.RegisteredClaims) { c.ExpiresAt = gojwt.NewNumericDate(now.Add(-time.Minute)) }),
		),
		"no expiry": sign(
			gojwt.SigningMethodHS256,
			secret,
			with(func(c *gojwt.RegisteredClaims) { c.ExpiresAt = nil }),
		),
		"wrong issuer": sign(
			gojwt.SigningMethodHS256,
			secret,
			with(func(c *gojwt.RegisteredClaims) { c.Issuer = "evil" }),
		),
		"wrong aud": sign(
			gojwt.SigningMethodHS256,
			secret,
			with(func(c *gojwt.RegisteredClaims) { c.Audience = gojwt.ClaimStrings{"other"} }),
		),
		"no subject": sign(
			gojwt.SigningMethodHS256,
			secret,
			with(func(c *gojwt.RegisteredClaims) { c.Subject = "" }),
		),
		"no audience": sign(
			gojwt.SigningMethodHS256,
			secret,
			with(func(c *gojwt.RegisteredClaims) { c.Audience = nil }),
		),
		"no issuer": sign(
			gojwt.SigningMethodHS256,
			secret,
			with(func(c *gojwt.RegisteredClaims) { c.Issuer = "" }),
		),
		"no iat": sign(
			gojwt.SigningMethodHS256,
			secret,
			with(func(c *gojwt.RegisteredClaims) { c.IssuedAt = nil }),
		),
		"lifetime too long": sign(
			gojwt.SigningMethodHS256,
			secret,
			with(func(c *gojwt.RegisteredClaims) { c.ExpiresAt = gojwt.NewNumericDate(now.Add(48 * time.Hour)) }),
		),
		"issued future": sign(
			gojwt.SigningMethodHS256,
			secret,
			with(func(c *gojwt.RegisteredClaims) { c.IssuedAt = gojwt.NewNumericDate(now.Add(time.Hour)) }),
		),
	}
	v := NewVerifier(testCfg)
	for name, token := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := v.Verify(token)
			require.ErrorIs(t, err, ErrInvalidToken)
		})
	}
}
