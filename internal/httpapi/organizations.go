package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

type OrganizationStore interface {
	List(ctx context.Context, f model.OrganizationFilter, limit, offset int) ([]model.Organization, error)
	Get(ctx context.Context, id int64) (model.Organization, error)
	Create(ctx context.Context, in model.Organization) (model.Organization, error)
	Update(ctx context.Context, id int64, in model.Organization) (model.Organization, error)
	Delete(ctx context.Context, id int64) error
}

type organizationHandlers struct{ s OrganizationStore }

func (h organizationHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/organizations", h.list)
	mux.HandleFunc("POST /api/v1/organizations", h.create)
	mux.HandleFunc("GET /api/v1/organizations/{id}", h.get)
	mux.HandleFunc("PUT /api/v1/organizations/{id}", h.update)
	mux.HandleFunc("DELETE /api/v1/organizations/{id}", h.delete)
}

func (h organizationHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, ok := parseList(w, r, nil, []string{"kind"})
	if !ok {
		return
	}
	items, err := h.s.List(r.Context(), model.OrganizationFilter{
		Kind: p.Text("kind"),
	}, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h organizationHandlers) get(w http.ResponseWriter, r *http.Request) {
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

func (h organizationHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in model.Organization
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

func (h organizationHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in model.Organization
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

func (h organizationHandlers) delete(w http.ResponseWriter, r *http.Request) {
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
