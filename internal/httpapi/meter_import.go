package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"dom-backend/internal/model"
	"dom-backend/internal/xlsximport"
)

type MeterReadingImportStore interface {
	ImportReadings(ctx context.Context, buildingID int64, resource string, date model.Date, installedAt *model.Date, rows []model.MeterReadingImportRow) (model.MeterReadingImportResult, error)
}

type meterImportHandlers struct{ store MeterReadingImportStore }

func (h meterImportHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/meter-readings/import", h.importFile)
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
// installed_at (если задано, неизвестные счётчики создаются с этой датой ввода) и xlsx-файл в поле file.
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
	res, err := h.store.ImportReadings(r.Context(), buildingID, resource, *date, installedAt, rows)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}
