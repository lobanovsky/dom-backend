package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"dom-backend/internal/model"
)

type AdjustmentRegistryStore interface {
	List(ctx context.Context, f model.AdjustmentRegistryFilter, limit, offset int) ([]model.AdjustmentRegistry, error)
	Get(ctx context.Context, id int64) (model.AdjustmentRegistry, error)
	File(ctx context.Context, id int64) (string, []byte, error)
	Create(ctx context.Context, req model.AdjustmentRegistryRequest) (model.AdjustmentRegistry, error)
	SetResolved(ctx context.Context, id int64, resolved bool) (model.AdjustmentRegistry, error)
	Delete(ctx context.Context, id int64) error
	Restore(ctx context.Context, id int64) (model.AdjustmentRegistry, error)
}

type adjustmentRegistryHandlers struct{ s AdjustmentRegistryStore }

func (h adjustmentRegistryHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/adjustment-registries", h.list)
	mux.HandleFunc("POST /api/v1/adjustment-registries", h.create)
	mux.HandleFunc("GET /api/v1/adjustment-registries/{id}", h.get)
	mux.HandleFunc("GET /api/v1/adjustment-registries/{id}/file", h.file)
	mux.HandleFunc("POST /api/v1/adjustment-registries/{id}/resolve", h.setResolved(true))
	mux.HandleFunc("POST /api/v1/adjustment-registries/{id}/unresolve", h.setResolved(false))
	mux.HandleFunc("POST /api/v1/adjustment-registries/{id}/restore", h.restore)
	mux.HandleFunc("DELETE /api/v1/adjustment-registries/{id}", h.remove)
}

func (h adjustmentRegistryHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, ok := parseListSpec(w, r, listSpec{Ints: []string{"bank_account_id", "personal_account_id"}, Bools: []string{"resolved"}})
	if !ok {
		return
	}
	f := model.AdjustmentRegistryFilter{Deleted: p.Deleted, BankAccountID: p.Int("bank_account_id"), PersonalAccountID: p.Int("personal_account_id"), Resolved: p.Bool("resolved")}
	items, err := h.s.List(r.Context(), f, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h adjustmentRegistryHandlers) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := h.s.Get(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h adjustmentRegistryHandlers) create(w http.ResponseWriter, r *http.Request) {
	var req model.AdjustmentRegistryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := req.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	item, err := h.s.Create(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h adjustmentRegistryHandlers) file(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	name, data, err := h.s.File(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=windows-1251")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

func (h adjustmentRegistryHandlers) setResolved(resolved bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		item, err := h.s.SetResolved(r.Context(), id, resolved)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func (h adjustmentRegistryHandlers) restore(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := h.s.Restore(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h adjustmentRegistryHandlers) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.s.Delete(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
