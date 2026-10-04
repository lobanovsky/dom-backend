package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

type OwnershipStore interface {
	List(ctx context.Context, f model.OwnershipFilter, limit, offset int) ([]model.Ownership, error)
	Get(ctx context.Context, id int64) (model.Ownership, error)
	Create(ctx context.Context, in model.Ownership) (model.Ownership, error)
	Update(ctx context.Context, id int64, in model.Ownership) (model.Ownership, error)
	Delete(ctx context.Context, id int64) error
}

type ownershipHandlers struct{ s OwnershipStore }

func (h ownershipHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/ownerships", h.list)
	mux.HandleFunc("POST /api/v1/ownerships", h.create)
	mux.HandleFunc("GET /api/v1/ownerships/{id}", h.get)
	mux.HandleFunc("PUT /api/v1/ownerships/{id}", h.update)
	mux.HandleFunc("DELETE /api/v1/ownerships/{id}", h.delete)
}

func (h ownershipHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, ok := parseList(w, r, []string{"premises_id", "person_id", "legal_entity_id"}, nil)
	if !ok {
		return
	}
	items, err := h.s.List(r.Context(), model.OwnershipFilter{
		PremisesID:    p.Int("premises_id"),
		PersonID:      p.Int("person_id"),
		LegalEntityID: p.Int("legal_entity_id"),
	}, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h ownershipHandlers) get(w http.ResponseWriter, r *http.Request) {
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

func (h ownershipHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in model.Ownership
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

func (h ownershipHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in model.Ownership
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

func (h ownershipHandlers) delete(w http.ResponseWriter, r *http.Request) {
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
