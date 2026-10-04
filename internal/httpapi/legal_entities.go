package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

type LegalEntityStore interface {
	List(ctx context.Context, f model.LegalEntityFilter, limit, offset int) ([]model.LegalEntity, error)
	Get(ctx context.Context, id int64) (model.LegalEntity, error)
	Create(ctx context.Context, in model.LegalEntity) (model.LegalEntity, error)
	Update(ctx context.Context, id int64, in model.LegalEntity) (model.LegalEntity, error)
	Delete(ctx context.Context, id int64) error
}

type legalEntityHandlers struct{ s LegalEntityStore }

func (h legalEntityHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/legal-entities", h.list)
	mux.HandleFunc("POST /api/v1/legal-entities", h.create)
	mux.HandleFunc("GET /api/v1/legal-entities/{id}", h.get)
	mux.HandleFunc("PUT /api/v1/legal-entities/{id}", h.update)
	mux.HandleFunc("DELETE /api/v1/legal-entities/{id}", h.delete)
}

func (h legalEntityHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, ok := parseList(w, r, nil, []string{"inn"})
	if !ok {
		return
	}
	items, err := h.s.List(r.Context(), model.LegalEntityFilter{
		INN: p.Text("inn"),
	}, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h legalEntityHandlers) get(w http.ResponseWriter, r *http.Request) {
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

func (h legalEntityHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in model.LegalEntity
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

func (h legalEntityHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in model.LegalEntity
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

func (h legalEntityHandlers) delete(w http.ResponseWriter, r *http.Request) {
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
