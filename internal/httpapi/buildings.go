package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

type BuildingStore interface {
	List(ctx context.Context, f model.BuildingFilter, limit, offset int) ([]model.Building, error)
	Get(ctx context.Context, id int64) (model.Building, error)
	Create(ctx context.Context, in model.Building) (model.Building, error)
	Update(ctx context.Context, id int64, in model.Building) (model.Building, error)
	Delete(ctx context.Context, id int64) error
	Restore(ctx context.Context, id int64) (model.Building, error)
}

type buildingHandlers struct{ s BuildingStore }

func (h buildingHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/buildings", h.list)
	mux.HandleFunc("POST /api/v1/buildings", h.create)
	mux.HandleFunc("GET /api/v1/buildings/{id}", h.get)
	mux.HandleFunc("PUT /api/v1/buildings/{id}", h.update)
	mux.HandleFunc("DELETE /api/v1/buildings/{id}", h.delete)
	mux.HandleFunc("POST /api/v1/buildings/{id}/restore", h.restore)
}

func (h buildingHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, ok := parseList(w, r, []string{"organization_id"}, []string{"kind"})
	if !ok {
		return
	}
	items, err := h.s.List(r.Context(), model.BuildingFilter{
		Deleted:        p.Deleted,
		OrganizationID: p.Int("organization_id"),
		Kind:           p.Text("kind"),
	}, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h buildingHandlers) get(w http.ResponseWriter, r *http.Request) {
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

func (h buildingHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in model.Building
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

func (h buildingHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in model.Building
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

func (h buildingHandlers) delete(w http.ResponseWriter, r *http.Request) {
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

func (h buildingHandlers) restore(w http.ResponseWriter, r *http.Request) {
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
