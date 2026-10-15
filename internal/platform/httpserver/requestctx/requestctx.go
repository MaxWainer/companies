package requestctx

import (
	"context"

	"github.com/google/uuid"

	authmodel "example.com/companies/internal/auth/model"
)

type key int

const (
	requestIDKey key = iota
	authKey
	inputKey
)

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

type Auth struct {
	Principal     authmodel.Principal
	Authenticated bool
	Failure       string
}

func WithAuth(ctx context.Context, a Auth) context.Context {
	return context.WithValue(ctx, authKey, a)
}

func AuthFrom(ctx context.Context) Auth {
	a, _ := ctx.Value(authKey).(Auth)
	return a
}

func Principal(ctx context.Context) (authmodel.Principal, bool) {
	a := AuthFrom(ctx)
	return a.Principal, a.Authenticated
}

type Input struct {
	Body any
	IDs  map[string]uuid.UUID
}

func WithInput(ctx context.Context, in Input) context.Context {
	return context.WithValue(ctx, inputKey, in)
}

func Body[T any](ctx context.Context) T {
	in, _ := ctx.Value(inputKey).(Input)
	body, _ := in.Body.(T)
	return body
}

func ID(ctx context.Context, name string) uuid.UUID {
	in, _ := ctx.Value(inputKey).(Input)
	return in.IDs[name]
}
