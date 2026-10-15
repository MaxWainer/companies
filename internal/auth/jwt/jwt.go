package jwt

import (
	"errors"
	"fmt"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	authmodel "example.com/companies/internal/auth/model"
	"example.com/companies/internal/platform/config"
)

var ErrInvalidToken = errors.New("invalid token")

const (
	leeway      = 30 * time.Second
	maxTokenTTL = 24 * time.Hour
)

type Verifier struct {
	secret []byte
	parser *gojwt.Parser
}

func NewVerifier(cfg config.Auth) *Verifier {
	parser := gojwt.NewParser(
		gojwt.WithValidMethods([]string{gojwt.SigningMethodHS256.Alg()}),
		gojwt.WithExpirationRequired(),
		gojwt.WithIssuedAt(),
		gojwt.WithLeeway(leeway),
		gojwt.WithIssuer(cfg.Issuer),
		gojwt.WithAudience(cfg.Audience),
	)
	return &Verifier{secret: []byte(cfg.JWTSecret), parser: parser}
}

func (v *Verifier) Verify(token string) (*authmodel.Principal, error) {
	var claims gojwt.RegisteredClaims
	_, err := v.parser.ParseWithClaims(token, &claims, func(*gojwt.Token) (any, error) {
		return v.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("%w: missing sub claim", ErrInvalidToken)
	}
	if claims.IssuedAt == nil {
		return nil, fmt.Errorf("%w: missing iat claim", ErrInvalidToken)
	}
	if claims.ExpiresAt.Sub(claims.IssuedAt.Time) > maxTokenTTL {
		return nil, fmt.Errorf("%w: lifetime exceeds %s", ErrInvalidToken, maxTokenTTL)
	}
	return &authmodel.Principal{Subject: claims.Subject}, nil
}

func Issue(cfg config.Auth, subject string, ttl time.Duration, now time.Time) (string, error) {
	claims := gojwt.RegisteredClaims{
		Subject:   subject,
		Issuer:    cfg.Issuer,
		IssuedAt:  gojwt.NewNumericDate(now),
		NotBefore: gojwt.NewNumericDate(now),
		ExpiresAt: gojwt.NewNumericDate(now.Add(ttl)),
	}
	claims.Audience = gojwt.ClaimStrings{cfg.Audience}
	token, err := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return token, nil
}
