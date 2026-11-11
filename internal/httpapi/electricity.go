package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"dom-backend/internal/model"
)

type ElectricityStore interface {
	CreateElectricityContract(context.Context, model.ElectricityContractUpdate) error
	ArchiveElectricityContract(context.Context, int64) error
	ListElectricityContracts(ctx context.Context) ([]model.ElectricityContract, error)
	UpdateElectricityContract(ctx context.Context, id int64, in model.ElectricityContractUpdate) error
	CalculateElectricity(ctx context.Context, period model.Date) (model.ElectricityCalculation, error)
}

type electricityHandlers struct{ s ElectricityStore }

func (h electricityHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/electricity-contracts", h.list)
	mux.HandleFunc("POST /api/v1/electricity-contracts", h.create)
	mux.HandleFunc("DELETE /api/v1/electricity-contracts/{id}", h.archive)
	mux.HandleFunc("PUT /api/v1/electricity-contracts/{id}", h.update)
	mux.HandleFunc("GET /api/v1/electricity-contracts/calculation", h.calculate)
}

func (h electricityHandlers) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.s.ListElectricityContracts(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h electricityHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in model.ElectricityContractUpdate
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.MeterIDs == nil {
		in.MeterIDs = []int64{}
	}
	if err := h.s.UpdateElectricityContract(r.Context(), id, in); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h electricityHandlers) calculate(w http.ResponseWriter, r *http.Request) {
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
	result, err := h.s.CalculateElectricity(r.Context(), model.Date{Time: t})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h electricityHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in model.ElectricityContractUpdate
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := h.s.CreateElectricityContract(r.Context(), in); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]bool{"ok": true})
}
func (h electricityHandlers) archive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.s.ArchiveElectricityContract(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(204)
}
