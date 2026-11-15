package httpapi

import (
	"context"
	"dom-backend/internal/model"
	"net/http"
)

type gateServiceStore interface {
	ServiceAccesses(context.Context) ([]model.GateServiceAccess, error)
	CreateServiceAccess(context.Context, model.User, model.GateServiceAccessInput) (model.GateServiceAccess, error)
	RevokeServiceZone(context.Context, model.User, int64, string) error
}

func (h gateHandlers) registerService(mux *http.ServeMux) {
	s, ok := h.s.(gateServiceStore)
	if !ok {
		return
	}
	admin := func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !currentUser(r).IsAdmin() {
				writeError(w, 403, "forbidden")
				return
			}
			fn(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/gate-service-accesses", admin(func(w http.ResponseWriter, r *http.Request) {
		items, err := s.ServiceAccesses(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	}))
	mux.HandleFunc("POST /api/v1/gate-service-accesses", admin(func(w http.ResponseWriter, r *http.Request) {
		var in model.GateServiceAccessInput
		if !decodeJSON(w, r, &in) {
			return
		}
		in.Normalize()
		if err := in.Validate(); err != nil {
			writeErr(w, err)
			return
		}
		item, err := s.CreateServiceAccess(r.Context(), currentUser(r), in)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 201, item)
	}))
	mux.HandleFunc("POST /api/v1/gate-service-accesses/{id}/{zone}/revoke", admin(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		if err := s.RevokeServiceZone(r.Context(), currentUser(r), id, r.PathValue("zone")); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}
