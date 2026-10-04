package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

type PersonStore interface {
	List(ctx context.Context, f model.PersonFilter, limit, offset int) ([]model.Person, error)
	Get(ctx context.Context, id int64) (model.Person, error)
	Create(ctx context.Context, in model.Person) (model.Person, error)
	Update(ctx context.Context, id int64, in model.Person) (model.Person, error)
	Delete(ctx context.Context, id int64) error
}

type personHandlers struct{ s PersonStore }

func (h personHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/persons", h.list)
	mux.HandleFunc("POST /api/v1/persons", h.create)
	mux.HandleFunc("GET /api/v1/persons/{id}", h.get)
	mux.HandleFunc("PUT /api/v1/persons/{id}", h.update)
	mux.HandleFunc("DELETE /api/v1/persons/{id}", h.delete)
}

func (h personHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, ok := parseList(w, r, nil, []string{"last_name", "phone"})
	if !ok {
		return
	}
	items, err := h.s.List(r.Context(), model.PersonFilter{
		LastName: p.Text("last_name"),
		Phone:    p.Text("phone"),
	}, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h personHandlers) get(w http.ResponseWriter, r *http.Request) {
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

func (h personHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in model.Person
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

func (h personHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in model.Person
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

func (h personHandlers) delete(w http.ResponseWriter, r *http.Request) {
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
