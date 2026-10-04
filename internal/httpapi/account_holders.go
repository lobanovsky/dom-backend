package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

type AccountHolderStore interface {
	List(ctx context.Context, f model.AccountHolderFilter, limit, offset int) ([]model.AccountHolder, error)
	Get(ctx context.Context, id int64) (model.AccountHolder, error)
	Create(ctx context.Context, in model.AccountHolder) (model.AccountHolder, error)
	Update(ctx context.Context, id int64, in model.AccountHolder) (model.AccountHolder, error)
	Delete(ctx context.Context, id int64) error
}

type accountHolderHandlers struct{ s AccountHolderStore }

func (h accountHolderHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/account-holders", h.list)
	mux.HandleFunc("POST /api/v1/account-holders", h.create)
	mux.HandleFunc("GET /api/v1/account-holders/{id}", h.get)
	mux.HandleFunc("PUT /api/v1/account-holders/{id}", h.update)
	mux.HandleFunc("DELETE /api/v1/account-holders/{id}", h.delete)
}

func (h accountHolderHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, ok := parseList(w, r, []string{"account_id", "person_id", "legal_entity_id"}, nil)
	if !ok {
		return
	}
	items, err := h.s.List(r.Context(), model.AccountHolderFilter{
		AccountID:     p.Int("account_id"),
		PersonID:      p.Int("person_id"),
		LegalEntityID: p.Int("legal_entity_id"),
	}, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h accountHolderHandlers) get(w http.ResponseWriter, r *http.Request) {
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

func (h accountHolderHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in model.AccountHolder
	if !decodeJSON(w, r, &in) {
		return
	}
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

func (h accountHolderHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in model.AccountHolder
	if !decodeJSON(w, r, &in) {
		return
	}
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

func (h accountHolderHandlers) delete(w http.ResponseWriter, r *http.Request) {
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
