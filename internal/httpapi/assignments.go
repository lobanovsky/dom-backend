package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

type AssignmentStore interface {
	Preview(ctx context.Context, req model.AssignRequest) (model.AssignPreview, error)
	Apply(ctx context.Context, req model.AssignRequest) (model.AssignResult, error)
	Rollback(ctx context.Context, runID int64) (model.RollbackResult, error)
	Runs(ctx context.Context, limit, offset int) ([]model.AssignRun, error)
}

// RuleOrderStore меняет порядок правил.
type RuleOrderStore interface {
	Reorder(ctx context.Context, ids []int64) error
}

type assignmentHandlers struct {
	s     AssignmentStore
	order RuleOrderStore
}

func (h assignmentHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/payment-assignments/preview", h.preview)
	mux.HandleFunc("POST /api/v1/payment-assignments", h.apply)
	mux.HandleFunc("GET /api/v1/payment-assignments", h.runs)
	mux.HandleFunc("POST /api/v1/payment-assignments/{id}/rollback", h.rollback)
	mux.HandleFunc("POST /api/v1/payment-rules/reorder", h.reorder)
}

func (h assignmentHandlers) preview(w http.ResponseWriter, r *http.Request) {
	var req model.AssignRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := h.s.Preview(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h assignmentHandlers) apply(w http.ResponseWriter, r *http.Request) {
	var req model.AssignRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := h.s.Apply(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	status := http.StatusCreated
	if res.RunID == 0 {
		status = http.StatusOK // менять было нечего, запуск не создан
	}
	writeJSON(w, status, res)
}

func (h assignmentHandlers) runs(w http.ResponseWriter, r *http.Request) {
	p, ok := parseList(w, r, nil, nil)
	if !ok {
		return
	}
	items, err := h.s.Runs(r.Context(), p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h assignmentHandlers) rollback(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := h.s.Rollback(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h assignmentHandlers) reorder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.IDs) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "ids: is required")
		return
	}
	if err := h.order.Reorder(r.Context(), body.IDs); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
