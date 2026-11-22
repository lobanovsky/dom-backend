package httpapi

import (
	"context"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"dom-backend/internal/model"
	"dom-backend/internal/xlsximport"
)

type MeterReadingImportStore interface {
	ImportReadings(ctx context.Context, buildingID int64, resource string, date, period model.Date, installedAt *model.Date, rows []model.MeterReadingImportRow) (model.MeterReadingImportResult, error)
	ImportUM31Readings(ctx context.Context, buildingID int64, averageMonths int, confirmHighUsage bool, fileName string, parseErrors []model.ImportRowError, rows []model.UM31ReadingImportRow) (model.MeterReadingImportResult, error)
	ListUM31ImportHistory(ctx context.Context, buildingID *int64, limit int) ([]model.MeterImportHistoryItem, error)
	UndoUM31Import(ctx context.Context, importRunID int64) (int, error)
}

type meterImportHandlers struct{ store MeterReadingImportStore }

func (h meterImportHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/meter-readings/import", h.importFile)
	mux.HandleFunc("POST /api/v1/meter-readings/import-um31", h.importUM31)
	mux.HandleFunc("GET /api/v1/meter-readings/import-um31/history", h.importUM31History)
	mux.HandleFunc("POST /api/v1/meter-readings/import-um31/history/{id}/undo", h.undoUM31Import)
}

func (h meterImportHandlers) undoUM31Import(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	removed, err := h.store.UndoUM31Import(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed_readings": removed})
}

func (h meterImportHandlers) importUM31(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportSize)
	if err := r.ParseMultipartForm(maxImportSize); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart body: "+err.Error())
		return
	}
	buildingID, err := strconv.ParseInt(r.FormValue("building_id"), 10, 64)
	if err != nil || buildingID <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "building_id: must be a positive integer")
		return
	}
	averageMonths := 1
	if value := strings.TrimSpace(r.FormValue("average_months")); value != "" {
		averageMonths, err = strconv.Atoi(value)
	}
	if err != nil || averageMonths < 1 || averageMonths > 12 {
		writeError(w, http.StatusUnprocessableEntity, "average_months: must be between 1 and 12")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, `field "file" with an xls file is required`)
		return
	}
	defer file.Close()
	rows, rowErrs, err := xlsximport.ParseUM31(file)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(rows) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "В файле нет корректных строк с показаниями счётчиков.", "rows": rowErrs})
		return
	}
	confirmHighUsage, err := strconv.ParseBool(r.FormValue("confirm_high_usage"))
	if err != nil && strings.TrimSpace(r.FormValue("confirm_high_usage")) != "" {
		writeError(w, http.StatusUnprocessableEntity, "confirm_high_usage: must be true or false")
		return
	}
	res, err := h.store.ImportUM31Readings(r.Context(), buildingID, averageMonths, confirmHighUsage, filepath.Base(header.Filename), rowErrs, rows)
	if err != nil {
		writeErr(w, err)
		return
	}
	if res.RequiresHighUsageConfirmation {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":                   "unusually high meter usage requires confirmation",
			"high_usage_warnings":     res.HighUsageWarnings,
			"lower_reading_estimates": res.LowerReadingEstimates,
			"issues":                  res.Issues,
		})
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (h meterImportHandlers) importUM31History(w http.ResponseWriter, r *http.Request) {
	var buildingID *int64
	if value := strings.TrimSpace(r.URL.Query().Get("building_id")); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusUnprocessableEntity, "building_id: must be a positive integer")
			return
		}
		buildingID = &id
	}
	items, err := h.store.ListUM31ImportHistory(r.Context(), buildingID, 50)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func formDate(w http.ResponseWriter, r *http.Request, field string, required bool) (*model.Date, bool) {
	v := strings.TrimSpace(r.FormValue(field))
	if v == "" {
		if required {
			writeError(w, http.StatusUnprocessableEntity, field+": is required")
			return nil, false
		}
		return nil, true
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, field+": date must be YYYY-MM-DD")
		return nil, false
	}
	d := model.Date{Time: t}
	return &d, true
}

// importFile принимает multipart/form-data: building_id, resource, reading_date (дата показаний), необязательное
// period (расчётный месяц YYYY-MM; по умолчанию месяц даты показаний), необязательное installed_at (если задано, неизвестные счётчики создаются с этой датой ввода) и xlsx-файл в поле file.
func (h meterImportHandlers) importFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportSize)
	if err := r.ParseMultipartForm(maxImportSize); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart body: "+err.Error())
		return
	}
	buildingID, err := strconv.ParseInt(r.FormValue("building_id"), 10, 64)
	if err != nil || buildingID <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "building_id: must be a positive integer")
		return
	}
	resource := r.FormValue("resource")
	if !slices.Contains(model.MeterResources, resource) {
		writeError(w, http.StatusUnprocessableEntity, "resource: must be one of: "+strings.Join(model.MeterResources, ", "))
		return
	}
	date, ok := formDate(w, r, "reading_date", true)
	if !ok {
		return
	}
	period := date.FirstOfMonth()
	if v := strings.TrimSpace(r.FormValue("period")); v != "" {
		if len(v) == 7 { // YYYY-MM из поля выбора месяца
			v += "-01"
		}
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "period: month must be YYYY-MM")
			return
		}
		period = model.Date{Time: t}.FirstOfMonth()
	}
	installedAt, ok := formDate(w, r, "installed_at", false)
	if !ok {
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, `field "file" with an xlsx file is required`)
		return
	}
	defer file.Close()

	rows, rowErrs, err := xlsximport.ParseReadings(file, resource)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(rowErrs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "file contains invalid rows, nothing was imported",
			"rows":  rowErrs,
		})
		return
	}
	res, err := h.store.ImportReadings(r.Context(), buildingID, resource, *date, period, installedAt, rows)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}
