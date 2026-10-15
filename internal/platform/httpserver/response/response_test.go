package response

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"example.com/companies/internal/platform/httpserver/requestctx"
	"example.com/companies/internal/platform/httpserver/response/model"
	"example.com/companies/internal/platform/logging/logtest"
)

func write(t *testing.T, ctx context.Context, err error) (*httptest.ResponseRecorder, *logtest.Recorder) {
	t.Helper()
	logger, logs := logtest.New()
	req := httptest.NewRequestWithContext(requestctx.WithRequestID(ctx, "req-1"), http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()
	WriteError(rec, req, logger, err)
	return rec, logs
}

func TestWriteErrorUsesTheErrorsOwnRepresentation(t *testing.T) {
	rec, logs := write(t, t.Context(), Validation(map[string]string{"name": "is required"}))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body model.ErrorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, model.ErrorDetail{
		Code: CodeValidation, Message: "request validation failed",
		Fields: map[string]string{"name": "is required"}, RequestID: "req-1",
	}, body.Error)
	assert.Zero(t, logs.Len(), "client errors are not logged as failures")
}

func TestWriteErrorHidesAndLogsUnexpectedErrors(t *testing.T) {
	rec, logs := write(t, t.Context(), assert.AnError)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), assert.AnError.Error())

	entries := logs.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "request failed", entries[0].Message)
	assert.Equal(t, slog.LevelError, entries[0].Level)
	assert.Equal(t, assert.AnError, entries[0].Attrs["error"])
}

func TestWriteErrorSkipsCancelledRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rec, logs := write(t, ctx, context.Canceled)
	assert.Zero(t, rec.Body.Len(), "nobody is listening")
	for _, e := range logs.Entries() {
		assert.Less(t, e.Level, slog.LevelError, e.Message)
	}
}

func TestWriteErrorTurnsAnExpiredDeadlineInto503(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 0)
	defer cancel()
	<-ctx.Done()
	rec, logs := write(t, ctx, fmt.Errorf("query: %w", ctx.Err()))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body model.ErrorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, CodeTimeout, body.Error.Code)
	entries := logs.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, slog.LevelWarn, entries[0].Level)
}

func TestWriteErrorTreatsForeignDeadlinesAsInternal(t *testing.T) {
	rec, _ := write(t, t.Context(), context.DeadlineExceeded)
	assert.Equal(t, http.StatusInternalServerError, rec.Code, "only the request's own deadline is a timeout")
}
