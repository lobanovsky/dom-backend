package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

// PropertiesStore отдаёт помещения, которыми владеет физлицо или юрлицо.
type PropertiesStore interface {
	ByPerson(ctx context.Context, id int64) ([]model.OwnedPremises, error)
	ByLegalEntity(ctx context.Context, id int64) ([]model.OwnedPremises, error)
}

type propertiesHandlers struct{ store PropertiesStore }

func (h propertiesHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/persons/{id}/properties", h.serve(func(ctx context.Context, id int64) ([]model.OwnedPremises, error) { return h.store.ByPerson(ctx, id) }))
	mux.HandleFunc("GET /api/v1/legal-entities/{id}/properties", h.serve(func(ctx context.Context, id int64) ([]model.OwnedPremises, error) {
		return h.store.ByLegalEntity(ctx, id)
	}))
}

func (h propertiesHandlers) serve(load func(context.Context, int64) ([]model.OwnedPremises, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		items, err := load(r.Context(), id)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}
