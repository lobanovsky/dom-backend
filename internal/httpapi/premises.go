package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

type PremisesStore interface {
	List(ctx context.Context, f model.PremisesFilter, limit, offset int) ([]model.Premises, error)
	Get(ctx context.Context, id int64) (model.Premises, error)
	Create(ctx context.Context, in model.Premises) (model.Premises, error)
	Update(ctx context.Context, id int64, in model.Premises) (model.Premises, error)
	Delete(ctx context.Context, id int64) error
}

type premisesHandlers struct{ s PremisesStore }

func (h premisesHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/premises", h.list)
	mux.HandleFunc("POST /api/v1/premises", h.create)
	mux.HandleFunc("GET /api/v1/premises/{id}", h.get)
	mux.HandleFunc("PUT /api/v1/premises/{id}", h.update)
	mux.HandleFunc("DELETE /api/v1/premises/{id}", h.delete)
}

func (h premisesHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, ok := parseList(w, r, []string{"building_id"}, []string{"kind", "number"})
	if !ok {
		return
	}
	items, err := h.s.List(r.Context(), model.PremisesFilter{
		BuildingID: p.Int("building_id"),
		Kind:       p.Text("kind"),
		Number:     p.Text("number"),
	}, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h premisesHandlers) get(w http.ResponseWriter, r *http.Request) {
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

func (h premisesHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in model.Premises
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

func (h premisesHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in model.Premises
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

func (h premisesHandlers) delete(w http.ResponseWriter, r *http.Request) {
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
