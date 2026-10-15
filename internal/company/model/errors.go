package model

import (
	"errors"
	"maps"
	"slices"
	"strings"
)

var (
	ErrNotFound  = errors.New("company not found")
	ErrNameTaken = errors.New("company name is already taken")
)

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	var parts []string
	for _, name := range slices.Sorted(maps.Keys(e.Fields)) {
		parts = append(parts, name+": "+e.Fields[name])
	}
	return "invalid company: " + strings.Join(parts, "; ")
}

func (e *ValidationError) FieldErrors() map[string]string { return e.Fields }

func NewValidationError(field, reason string) *ValidationError {
	return &ValidationError{Fields: map[string]string{field: reason}}
}

func (e *ValidationError) check(field, reason string) {
	if reason == "" {
		return
	}
	if e.Fields == nil {
		e.Fields = map[string]string{}
	}
	e.Fields[field] = reason
}

func (e *ValidationError) orNil() error {
	if len(e.Fields) == 0 {
		return nil
	}
	return e
}

func Outcome(err error) string {
	switch {
	case errors.As(err, new(*ValidationError)):
		return "validation"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrNameTaken):
		return "name_taken"
	default:
		return ""
	}
}
