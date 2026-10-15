package model

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

func validateName(name string) string {
	n := utf8.RuneCountInString(name)
	switch {
	case n == 0:
		return "must not be empty"
	case n > MaxNameLength:
		return fmt.Sprintf("must be at most %d characters", MaxNameLength)
	case strings.ContainsFunc(name, unicode.IsControl):
		return "must not contain control characters"
	}
	return ""
}

func validateDescription(desc *string) string {
	if desc == nil {
		return ""
	}
	switch {
	case utf8.RuneCountInString(*desc) > MaxDescriptionLength:
		return fmt.Sprintf("must be at most %d characters", MaxDescriptionLength)
	case strings.ContainsRune(*desc, 0):
		return "must not contain NUL characters"
	}
	return ""
}

func validateEmployees(n int) string {
	if n < 0 || n > MaxEmployees {
		return fmt.Sprintf("must be between 0 and %d", MaxEmployees)
	}
	return ""
}

func validateType(t Type) string {
	if t.Valid() {
		return ""
	}
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = fmt.Sprintf("%q", t)
	}
	return "must be one of " + strings.Join(names, ", ")
}

func normalizeName(name string) string { return strings.TrimSpace(name) }
