package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

type AccountStore interface {
	List(ctx context.Context, f model.AccountFilter, limit, offset int) ([]model.Account, error)
	Get(ctx context.Context, id int64) (model.Account, error)
	Create(ctx context.Context, in model.Account) (model.Account, error)
	Update(ctx context.Context, id int64, in model.Account) (model.Account, error)
	Delete(ctx context.Context, id int64) error
}

type accountHandlers struct{ s AccountStore }

func (h accountHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/accounts", h.list)
	mux.HandleFunc("POST /api/v1/accounts", h.create)
	mux.HandleFunc("GET /api/v1/accounts/{id}", h.get)
	mux.HandleFunc("PUT /api/v1/accounts/{id}", h.update)
	mux.HandleFunc("DELETE /api/v1/accounts/{id}", h.delete)
}

func (h accountHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, ok := parseList(w, r, []string{"premises_id"}, []string{"number", "status", "purpose"})
	if !ok {
		return
	}
	items, err := h.s.List(r.Context(), model.AccountFilter{
		PremisesID: p.Int("premises_id"),
		Number:     p.Text("number"),
		Status:     p.Text("status"),
		Purpose:    p.Text("purpose"),
	}, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h accountHandlers) get(w http.ResponseWriter, r *http.Request) {
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

func (h accountHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in model.Account
	if !decodeJSON(w, r, &in) {
		return
	}
	in.SetDefaults()
	if err := in.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	item, err := h.s.Create(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h accountHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in model.Account
	if !decodeJSON(w, r, &in) {
		return
	}
	in.SetDefaults()
	if err := in.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	item, err := h.s.Update(r.Context(), id, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h accountHandlers) delete(w http.ResponseWriter, r *http.Request) {
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
