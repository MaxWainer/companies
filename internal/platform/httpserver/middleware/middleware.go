package middleware

import (
	"net/http"
	"slices"
)

type Middleware func(http.Handler) http.Handler

func Chain(mws ...Middleware) Middleware {
	return func(h http.Handler) http.Handler {
		for _, mw := range slices.Backward(mws) {
			h = mw(h)
		}
		return h
	}
}
