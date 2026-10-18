package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

// TariffFromServiceStore — вынос собственных цен услуги в тариф справочника (обычный CRUD идёт через crudHandlers).
type TariffFromServiceStore interface {
	FromService(ctx context.Context, req model.TariffFromServiceRequest) (model.Tariff, error)
}

type tariffFromServiceHandler struct{ s TariffFromServiceStore }

func (h tariffFromServiceHandler) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/tariffs/from-service", h.fromService)
}

func (h tariffFromServiceHandler) fromService(w http.ResponseWriter, r *http.Request) {
	var req model.TariffFromServiceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	t, err := h.s.FromService(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}
