package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"dom-backend/internal/hotwaterreport"
	"dom-backend/internal/model"
)

type WaterStore interface {
	SendHotWaterReport(context.Context, model.Date, func(context.Context) error) (time.Time, error)
	CreateWaterContract(context.Context, model.WaterContractUpdate) error
	ArchiveWaterContract(context.Context, int64) error
	ListWaterContracts(ctx context.Context) ([]model.WaterContract, error)
	UpdateWaterContract(ctx context.Context, id int64, in model.WaterContractUpdate) error
	CalculateWater(ctx context.Context, period model.Date) (model.WaterCalculation, error)
}

type waterHandlers struct {
	s          WaterStore
	sendReport func(context.Context, time.Time, []byte) error
}

func (h waterHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/water-contracts", h.list)
	mux.HandleFunc("POST /api/v1/water-contracts", h.create)
	mux.HandleFunc("DELETE /api/v1/water-contracts/{id}", h.archive)
	mux.HandleFunc("PUT /api/v1/water-contracts/{id}", h.update)
	mux.HandleFunc("GET /api/v1/water-contracts/calculation", h.calculate)
	mux.HandleFunc("GET /api/v1/water-contracts/hot-water-report", h.hotWaterReport)
	mux.HandleFunc("POST /api/v1/water-contracts/hot-water-report/send", h.sendHotWaterReport)
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

func waterPeriod(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	if len(r.URL.Query()) != 1 || len(r.URL.Query()["period"]) != 1 {
		writeError(w, 422, "period: must be YYYY-MM")
		return time.Time{}, false
	}
	v := strings.TrimSpace(r.URL.Query().Get("period"))
	t, err := time.Parse("2006-01", v)
	if len(v) != 7 || err != nil || t.Year() < 1 {
		writeError(w, 422, "period: must be YYYY-MM")
		return time.Time{}, false
	}
	return t, true
}

func (h waterHandlers) calculate(w http.ResponseWriter, r *http.Request) {
	t, ok := waterPeriod(w, r)
	if !ok {
		return
	}
	result, err := h.s.CalculateWater(r.Context(), model.Date{Time: t})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h waterHandlers) hotWaterReport(w http.ResponseWriter, r *http.Request) {
	period, ok := waterPeriod(w, r)
	if !ok {
		return
	}
	data, ok := h.renderHotWaterReport(w, r, period)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+period.Format("2006-01")+`-hot-water-report.pdf"`)
	_, _ = w.Write(data)
}

func (h waterHandlers) renderHotWaterReport(w http.ResponseWriter, r *http.Request, period time.Time) ([]byte, bool) {
	result, err := h.s.CalculateWater(r.Context(), model.Date{Time: period})
	if err != nil {
		writeErr(w, err)
		return nil, false
	}
	volumes := result.Volumes
	if volumes.ReportApartmentHot == nil || volumes.ReportOfficeHot == nil {
		writeError(w, http.StatusUnprocessableEntity, "Не хватает данных ГВС за выбранный месяц для формирования справки.")
		return nil, false
	}
	data, err := hotwaterreport.Render(period, *volumes.ReportApartmentHot, *volumes.ReportOfficeHot)
	if err != nil {
		writeErr(w, err)
		return nil, false
	}
	return data, true
}

func (h waterHandlers) sendHotWaterReport(w http.ResponseWriter, r *http.Request) {
	period, ok := waterPeriod(w, r)
	if !ok {
		return
	}
	if h.sendReport == nil {
		writeError(w, http.StatusServiceUnavailable, "Отправка справок по почте не настроена.")
		return
	}
	data, ok := h.renderHotWaterReport(w, r, period)
	if !ok {
		return
	}
	sentAt, err := h.s.SendHotWaterReport(r.Context(), model.Date{Time: period}, func(ctx context.Context) error {
		return h.sendReport(ctx, period, data)
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]time.Time{"sent_at": sentAt})
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
