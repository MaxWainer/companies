package request

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/mailru/easyjson"
	"github.com/mailru/easyjson/jlexer"

	"example.com/companies/internal/platform/httpserver/response"
)

var errEmptyBody = errors.New("request body must not be empty")

func Decode(
	w http.ResponseWriter,
	r *http.Request,
	dst easyjson.Unmarshaler,
	maxBody int64,
	mediaTypes ...string,
) error {
	if err := checkContentType(r, mediaTypes); err != nil {
		return err
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		return decodeError(err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return decodeError(errEmptyBody)
	}
	in := jlexer.Lexer{Data: body}
	dst.UnmarshalEasyJSON(&in)
	in.Consumed()
	if err := in.Error(); err != nil {
		return decodeError(err)
	}
	return nil
}

func PathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, response.Validation(map[string]string{name: "must be a valid UUID"})
	}
	return id, nil
}

func checkContentType(r *http.Request, allowed []string) error {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err == nil && slices.Contains(allowed, mt) {
		return nil
	}
	return &response.Error{
		Status:  http.StatusUnsupportedMediaType,
		Code:    response.CodeUnsupportedMedia,
		Message: "Content-Type must be " + strings.Join(allowed, " or "),
	}
}

func decodeError(err error) error {
	var (
		fieldErr *FieldError
		lexErr   *jlexer.LexerError
		sizeErr  *http.MaxBytesError
	)
	switch {
	case errors.As(err, &fieldErr):
		return response.Validation(map[string]string{fieldErr.Field: fieldErr.Reason})
	case errors.As(err, &sizeErr):
		return &response.Error{
			Status:  http.StatusRequestEntityTooLarge,
			Code:    response.CodeTooLarge,
			Message: fmt.Sprintf("request body must not exceed %d bytes", sizeErr.Limit),
		}
	case errors.Is(err, errEmptyBody), errors.Is(err, ErrNotObject):
		return response.BadRequest(err.Error())
	case errors.As(err, &lexErr), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return response.BadRequest("request body contains malformed JSON")
	default:
		return response.BadRequest("invalid request body")
	}
}
