package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

// AccountServiceBulkStore — массовые операции над назначениями услуг (обычный CRUD идёт через crudHandlers).
type AccountServiceBulkStore interface {
	BulkPreview(ctx context.Context, req model.AccountServiceBulkRequest) (model.AccountServiceBulkPreview, error)
	BulkAssign(ctx context.Context, req model.AccountServiceBulkRequest) (model.AccountServiceBulkResult, error)
	BulkClose(ctx context.Context, req model.AccountServiceCloseRequest) (model.AccountServiceCloseResult, error)
}

type accountServiceBulkHandlers struct{ s AccountServiceBulkStore }

func (h accountServiceBulkHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/account-services/bulk/preview", h.preview)
	mux.HandleFunc("POST /api/v1/account-services/bulk", h.assign)
	mux.HandleFunc("POST /api/v1/account-services/bulk-close", h.close)
}

func (h accountServiceBulkHandlers) preview(w http.ResponseWriter, r *http.Request) {
	var req model.AccountServiceBulkRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := h.s.BulkPreview(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h accountServiceBulkHandlers) assign(w http.ResponseWriter, r *http.Request) {
	var req model.AccountServiceBulkRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := h.s.BulkAssign(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (h accountServiceBulkHandlers) close(w http.ResponseWriter, r *http.Request) {
	var req model.AccountServiceCloseRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := h.s.BulkClose(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
