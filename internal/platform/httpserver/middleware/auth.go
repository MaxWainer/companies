package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	authmodel "example.com/companies/internal/auth/model"
	"example.com/companies/internal/platform/httpserver/requestctx"
	"example.com/companies/internal/platform/httpserver/response"
)

type TokenVerifier interface {
	Verify(token string) (*authmodel.Principal, error)
}

const (
	failureMissing = "missing bearer token"
	failureInvalid = "invalid or expired token"
)

func Authenticate(verifier TokenVerifier, logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			outcome := requestctx.Auth{Failure: failureMissing}
			if scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok &&
				strings.EqualFold(scheme, "Bearer") && strings.TrimSpace(token) != "" {
				principal, err := verifier.Verify(strings.TrimSpace(token))
				if err != nil {
					logger.DebugContext(r.Context(), "token rejected", slog.Any("error", err))
					outcome = requestctx.Auth{Failure: failureInvalid}
				} else {
					outcome = requestctx.Auth{Principal: *principal, Authenticated: true}
				}
			}
			next.ServeHTTP(w, r.WithContext(requestctx.WithAuth(r.Context(), outcome)))
		})
	}
}

func RequireAuth(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := requestctx.AuthFrom(r.Context())
		if !a.Authenticated {
			reason := a.Failure
			if reason == "" {
				reason = failureMissing
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="companies"`)
			response.WriteError(w, r, logger, &response.Error{
				Status: http.StatusUnauthorized, Code: response.CodeUnauthorized, Message: reason,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
