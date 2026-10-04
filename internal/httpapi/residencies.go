package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

type ResidencyStore interface {
	List(ctx context.Context, f model.ResidencyFilter, limit, offset int) ([]model.Residency, error)
	Get(ctx context.Context, id int64) (model.Residency, error)
	Create(ctx context.Context, in model.Residency) (model.Residency, error)
	Update(ctx context.Context, id int64, in model.Residency) (model.Residency, error)
	Delete(ctx context.Context, id int64) error
}

type residencyHandlers struct{ s ResidencyStore }

func (h residencyHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/residencies", h.list)
	mux.HandleFunc("POST /api/v1/residencies", h.create)
	mux.HandleFunc("GET /api/v1/residencies/{id}", h.get)
	mux.HandleFunc("PUT /api/v1/residencies/{id}", h.update)
	mux.HandleFunc("DELETE /api/v1/residencies/{id}", h.delete)
}

func (h residencyHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, ok := parseList(w, r, []string{"premises_id", "person_id", "related_owner_id"}, nil)
	if !ok {
		return
	}
	items, err := h.s.List(r.Context(), model.ResidencyFilter{
		PremisesID:     p.Int("premises_id"),
		PersonID:       p.Int("person_id"),
		RelatedOwnerID: p.Int("related_owner_id"),
	}, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h residencyHandlers) get(w http.ResponseWriter, r *http.Request) {
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

func (h residencyHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in model.Residency
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

func (h residencyHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in model.Residency
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

func (h residencyHandlers) delete(w http.ResponseWriter, r *http.Request) {
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
