package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"example.com/companies/internal/platform/httpserver/response"
)

func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity, as net/http does
					panic(rec)
				}
				logger.ErrorContext(r.Context(), "panic serving request",
					slog.Any("panic", rec), slog.String("stack", string(debug.Stack())))
				response.WriteError(w, r, logger, response.Internal())
			}()
			next.ServeHTTP(w, r)
		})
	}
}
