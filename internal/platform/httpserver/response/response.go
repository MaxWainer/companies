package response

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/mailru/easyjson"

	"example.com/companies/internal/platform/httpserver/requestctx"
	"example.com/companies/internal/platform/httpserver/response/model"
)

const (
	CodeBadRequest       = "bad_request"
	CodeValidation       = "validation_failed"
	CodeUnauthorized     = "unauthorized"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeTooLarge         = "payload_too_large"
	CodeUnsupportedMedia = "unsupported_media_type"
	CodeInternal         = "internal_error"
	CodeUnavailable      = "service_unavailable"
	CodeTimeout          = "timeout"
)

type Error struct {
	Status  int
	Code    string
	Message string
	Fields  map[string]string
}

func (e *Error) Error() string { return e.Message }

func BadRequest(msg string) *Error {
	return &Error{Status: http.StatusBadRequest, Code: CodeBadRequest, Message: msg}
}

func Validation(fields map[string]string) *Error {
	return &Error{
		Status:  http.StatusBadRequest,
		Code:    CodeValidation,
		Message: "request validation failed",
		Fields:  fields,
	}
}

func Internal() *Error {
	return &Error{Status: http.StatusInternalServerError, Code: CodeInternal, Message: "internal server error"}
}

type fieldErrors interface {
	error
	FieldErrors() map[string]string
}

func JSON(w http.ResponseWriter, status int, v easyjson.Marshaler) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = easyjson.MarshalToWriter(v, w)
}

func WriteError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var (
		e      *Error
		fields fieldErrors
	)
	switch {
	case errors.As(err, &e):
	case errors.As(err, &fields):
		e = Validation(fields.FieldErrors())
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		logger.DebugContext(r.Context(), "request cancelled by client")
		return
	case errors.Is(err, context.DeadlineExceeded) && r.Context().Err() != nil:
		logger.WarnContext(r.Context(), "request timed out", slog.Any("error", err))
		e = &Error{Status: http.StatusServiceUnavailable, Code: CodeTimeout, Message: "request timed out; retry later"}
	default:
		logger.ErrorContext(r.Context(), "request failed", slog.Any("error", err))
		e = Internal()
	}
	JSON(w, e.Status, model.ErrorBody{Error: model.ErrorDetail{
		Code:      e.Code,
		Message:   e.Message,
		Fields:    e.Fields,
		RequestID: requestctx.RequestID(r.Context()),
	}})
}
