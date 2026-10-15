package middleware

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/mailru/easyjson"

	"example.com/companies/internal/platform/httpserver/request"
	"example.com/companies/internal/platform/httpserver/requestctx"
	"example.com/companies/internal/platform/httpserver/response"
)

type Body interface {
	easyjson.Unmarshaler
	Validate() error
}

type Input struct {
	Body       func() Body
	MediaTypes []string
	PathUUIDs  []string
}

func Validate(in Input, maxBody int64, logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var val requestctx.Input
			if len(in.PathUUIDs) > 0 {
				val.IDs = make(map[string]uuid.UUID, len(in.PathUUIDs))
			}
			for _, name := range in.PathUUIDs {
				id, err := request.PathUUID(r, name)
				if err != nil {
					response.WriteError(w, r, logger, err)
					return
				}
				val.IDs[name] = id
			}
			if in.Body != nil {
				body := in.Body()
				if err := request.Decode(w, r, body, maxBody, in.MediaTypes...); err != nil {
					response.WriteError(w, r, logger, err)
					return
				}
				if err := body.Validate(); err != nil {
					response.WriteError(w, r, logger, err)
					return
				}
				val.Body = body
			}
			next.ServeHTTP(w, r.WithContext(requestctx.WithInput(r.Context(), val)))
		})
	}
}
