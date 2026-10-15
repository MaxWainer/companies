package request

import (
	"errors"
	"strconv"
	"strings"

	"github.com/mailru/easyjson/jlexer"
)

var ErrNotObject = errors.New("request body must be a JSON object")

type FieldError struct {
	Field  string
	Reason string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Reason }

func RejectField(in *jlexer.Lexer, name, reason string) {
	in.AddError(&FieldError{Field: strings.Clone(name), Reason: reason})
}

type Optional[T any] struct {
	Value T
	Set   bool
	Null  bool
}

func DecodeObject(in *jlexer.Lexer, member func(name string)) {
	if in.IsNull() {
		in.Skip()
		return
	}
	if in.CurrentToken() != jlexer.TokenDelim || !in.IsDelim('{') {
		if in.Ok() {
			in.AddError(ErrNotObject)
		}
		return
	}
	in.Delim('{')
	for !in.IsDelim('}') {
		name := in.UnsafeFieldName(false)
		in.WantColon()
		member(name)
		if !in.Ok() {
			return
		}
		in.WantComma()
	}
	in.Delim('}')
}

type Reader[T any] func(in *jlexer.Lexer, field string) T

func ReadPtr[T any](in *jlexer.Lexer, field string, read Reader[T]) *T {
	if in.IsNull() {
		in.Skip()
		return nil
	}
	v := read(in, field)
	return &v
}

func ReadOptional[T any](in *jlexer.Lexer, field string, read Reader[T]) Optional[T] {
	if in.IsNull() {
		in.Skip()
		return Optional[T]{Set: true, Null: true}
	}
	return Optional[T]{Value: read(in, field), Set: true}
}

func typeMismatch(in *jlexer.Lexer, field, kind string) {
	RejectField(in, field, "must be a JSON "+kind)
}

func ReadString(in *jlexer.Lexer, field string) string {
	if in.CurrentToken() != jlexer.TokenString {
		typeMismatch(in, field, "string")
		return ""
	}
	return in.String()
}

func ReadBool(in *jlexer.Lexer, field string) bool {
	if in.CurrentToken() != jlexer.TokenBool {
		typeMismatch(in, field, "boolean")
		return false
	}
	return in.Bool()
}

func ReadInt(in *jlexer.Lexer, field string) int {
	if in.CurrentToken() != jlexer.TokenNumber {
		typeMismatch(in, field, "integer")
		return 0
	}
	n, err := strconv.ParseInt(string(in.JsonNumber()), 10, strconv.IntSize)
	if err != nil {
		typeMismatch(in, field, "integer")
		return 0
	}
	return int(n)
}
