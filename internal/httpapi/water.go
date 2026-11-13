package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"dom-backend/internal/model"
)

type WaterStore interface {
	CreateWaterContract(context.Context, model.WaterContractUpdate) error
	ArchiveWaterContract(context.Context, int64) error
	ListWaterContracts(ctx context.Context) ([]model.WaterContract, error)
	UpdateWaterContract(ctx context.Context, id int64, in model.WaterContractUpdate) error
	CalculateWater(ctx context.Context, period model.Date) (model.WaterCalculation, error)
}

type waterHandlers struct{ s WaterStore }

func (h waterHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/water-contracts", h.list)
	mux.HandleFunc("POST /api/v1/water-contracts", h.create)
	mux.HandleFunc("DELETE /api/v1/water-contracts/{id}", h.archive)
	mux.HandleFunc("PUT /api/v1/water-contracts/{id}", h.update)
	mux.HandleFunc("GET /api/v1/water-contracts/calculation", h.calculate)
}

func (h waterHandlers) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.s.ListWaterContracts(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h waterHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in model.WaterContractUpdate
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	if err := h.s.UpdateWaterContract(r.Context(), id, in); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h waterHandlers) calculate(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 1 || len(r.URL.Query()["period"]) != 1 {
		writeError(w, 422, "period: must be YYYY-MM")
		return
	}
	v := strings.TrimSpace(r.URL.Query().Get("period"))
	t, err := time.Parse("2006-01", v)
	if len(v) != 7 || err != nil || t.Year() < 1 {
		writeError(w, 422, "period: must be YYYY-MM")
		return
	}
	result, err := h.s.CalculateWater(r.Context(), model.Date{Time: t})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h waterHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in model.WaterContractUpdate
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	if err := h.s.CreateWaterContract(r.Context(), in); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]bool{"ok": true})
}
func (h waterHandlers) archive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.s.ArchiveWaterContract(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(204)
}
