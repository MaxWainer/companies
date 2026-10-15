package handler

import (
	"errors"
	"log/slog"
	"net/http"

	dto "example.com/companies/internal/company/handler/model"
	"example.com/companies/internal/company/model"
	"example.com/companies/internal/company/service"
	eventmodel "example.com/companies/internal/event/model"
	"example.com/companies/internal/platform/httpserver"
	"example.com/companies/internal/platform/httpserver/middleware"
	"example.com/companies/internal/platform/httpserver/requestctx"
	"example.com/companies/internal/platform/httpserver/response"
	responsemodel "example.com/companies/internal/platform/httpserver/response/model"
)

var _ responsemodel.InternalError

const (
	collectionPath = "/v1/companies"
	itemPath       = collectionPath + "/{id}"
	mediaJSON      = "application/json"
	mediaMergeJSON = "application/merge-patch+json"
)

type Handler struct {
	svc    service.Companies
	logger *slog.Logger
}

func New(svc service.Companies, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Handler{svc: svc, logger: logger}
}

func (h *Handler) Routes() []httpserver.Route {
	byID := &middleware.Input{PathUUIDs: []string{"id"}}
	return []httpserver.Route{
		{
			Method: http.MethodPost, Path: collectionPath, Handler: h.create,
			Input: &middleware.Input{
				Body:       func() middleware.Body { return &dto.CreateRequest{} },
				MediaTypes: []string{mediaJSON},
			},
		},
		{Method: http.MethodGet, Path: itemPath, Handler: h.get, Public: true, Input: byID},
		{
			Method: http.MethodPatch, Path: itemPath, Handler: h.patch,
			Input: &middleware.Input{
				Body:       func() middleware.Body { return &dto.PatchRequest{} },
				MediaTypes: []string{mediaJSON, mediaMergeJSON},
				PathUUIDs:  []string{"id"},
			},
		},
		{Method: http.MethodDelete, Path: itemPath, Handler: h.delete, Input: byID},
	}
}

// @Summary	Create a company
// @Tags		companies
// @Accept		json
// @Produce	json
// @Security	BearerAuth
// @Param		company	body		dto.CreateRequest	true	"New company"
// @Success	201		{object}	model.Company		"Created; Location header points at it"
// @Failure	400		{object}	responsemodel.ValidationFailedError		"Malformed body (bad_request) or invalid fields (validation_failed)"
// @Failure	401		{object}	responsemodel.UnauthorizedError			"Missing, invalid or expired token"
// @Failure	409		{object}	responsemodel.ConflictError				"Name already taken (case-insensitive)"
// @Failure	413		{object}	responsemodel.PayloadTooLargeError		"Body too large"
// @Failure	415		{object}	responsemodel.UnsupportedMediaTypeError	"Content-Type is not application/json"
// @Failure	500		{object}	responsemodel.InternalError				"Unexpected failure"
// @Failure	503	{object}	responsemodel.ServiceUnavailableError	"Event broker unavailable (service_unavailable) or request timed out (timeout); nothing was saved"
// @Router		/v1/companies [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	req := requestctx.Body[*dto.CreateRequest](r.Context())
	c, err := h.svc.Create(r.Context(), actor(r), req.Params())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", collectionPath+"/"+c.ID.String())
	response.JSON(w, http.StatusCreated, c)
}

// @Summary	Get a company
// @Tags		companies
// @Produce	json
// @Param		id	path		string	true	"Company id"	format(uuid)
// @Success	200	{object}	model.Company
// @Failure	400	{object}	responsemodel.InvalidIDError	"Invalid id"
// @Failure	404	{object}	responsemodel.NotFoundError		"Not found"
// @Failure	500	{object}	responsemodel.InternalError		"Unexpected failure"
// @Failure	503	{object}	responsemodel.TimeoutError		"Request timed out"
// @Router		/v1/companies/{id} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.Get(r.Context(), requestctx.ID(r.Context(), "id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, c)
}

// @Summary		Partially update a company
// @Description	JSON Merge Patch: only sent members change; "description": null clears it; null is rejected for required attributes.
// @Tags			companies
// @Accept			json,application/merge-patch+json
// @Produce		json
// @Security		BearerAuth
// @Param			id		path		string				true	"Company id"	format(uuid)
// @Param			patch	body		dto.PatchRequest	true	"Members to change"
// @Success		200		{object}	model.Company
// @Failure		400		{object}	responsemodel.ValidationFailedError		"Malformed body (bad_request) or invalid id/fields (validation_failed)"
// @Failure		401		{object}	responsemodel.UnauthorizedError			"Missing, invalid or expired token"
// @Failure		404		{object}	responsemodel.NotFoundError				"Not found"
// @Failure		409		{object}	responsemodel.ConflictError				"Name already taken"
// @Failure		413		{object}	responsemodel.PayloadTooLargeError		"Body too large"
// @Failure		415		{object}	responsemodel.UnsupportedMediaTypeError	"Content-Type is not JSON or merge-patch JSON"
// @Failure		500		{object}	responsemodel.InternalError				"Unexpected failure"
// @Failure	503	{object}	responsemodel.ServiceUnavailableError	"Event broker unavailable (service_unavailable) or request timed out (timeout); nothing was saved"
// @Router			/v1/companies/{id} [patch]
func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	id := requestctx.ID(r.Context(), "id")
	req := requestctx.Body[*dto.PatchRequest](r.Context())
	c, err := h.svc.Patch(r.Context(), actor(r), id, req.Params())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, c)
}

// @Summary	Delete a company
// @Tags		companies
// @Security	BearerAuth
// @Param		id	path	string	true	"Company id"	format(uuid)
// @Success	204
// @Failure	400	{object}	responsemodel.InvalidIDError	"Invalid id"
// @Failure	401	{object}	responsemodel.UnauthorizedError	"Missing, invalid or expired token"
// @Failure	404	{object}	responsemodel.NotFoundError		"Not found"
// @Failure	500	{object}	responsemodel.InternalError		"Unexpected failure"
// @Failure	503	{object}	responsemodel.ServiceUnavailableError	"Event broker unavailable (service_unavailable) or request timed out (timeout); nothing was saved"
// @Router		/v1/companies/{id} [delete]
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), actor(r), requestctx.ID(r.Context(), "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, model.ErrNotFound):
		err = &response.Error{Status: http.StatusNotFound, Code: response.CodeNotFound, Message: err.Error()}
	case errors.Is(err, eventmodel.ErrPublishFailed):
		err = &response.Error{
			Status: http.StatusServiceUnavailable, Code: response.CodeUnavailable,
			Message: "the change was not saved because its event could not be published; retry later",
		}
	case errors.Is(err, model.ErrNameTaken):
		err = &response.Error{
			Status: http.StatusConflict, Code: response.CodeConflict, Message: err.Error(),
			Fields: map[string]string{"name": "is already taken"},
		}
	}
	response.WriteError(w, r, h.logger, err)
}

func actor(r *http.Request) string {
	p, _ := requestctx.Principal(r.Context())
	return p.Subject
}
