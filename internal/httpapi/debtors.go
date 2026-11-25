package httpapi

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"dom-backend/internal/model"
	"dom-backend/internal/xlsximport"
)

type DebtorsStore interface {
	Import(context.Context, model.Date, string, []model.DebtorImportRow) (model.DebtorImportResult, error)
	ListReports(context.Context) ([]model.DebtorReport, error)
	List(context.Context, model.Date) ([]model.DebtorReportRow, error)
}

type debtorHandlers struct{ store DebtorsStore }

func (h debtorHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/debtors/reports", h.reports)
	mux.HandleFunc("GET /api/v1/debtors", h.list)
	mux.HandleFunc("POST /api/v1/debtors/import", h.importFile)
}

func (h debtorHandlers) reports(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListReports(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h debtorHandlers) list(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 1 || r.URL.Query().Get("period") == "" {
		writeError(w, http.StatusBadRequest, `period=YYYY-MM is required`)
		return
	}
	period, err := parseDebtorPeriod(r.URL.Query().Get("period"))
	if err != nil {
		writeError(w, http.StatusBadRequest, `period must have format YYYY-MM`)
		return
	}
	items, err := h.store.List(r.Context(), period)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h debtorHandlers) importFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportSize)
	if err := r.ParseMultipartForm(maxImportSize); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart body")
		return
	}
	period, err := parseDebtorPeriod(strings.TrimSpace(r.FormValue("period")))
	if err != nil {
		writeError(w, http.StatusBadRequest, `period must have format YYYY-MM`)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, `field "file" with an xlsx file is required`)
		return
	}
	defer file.Close()
	items, err := xlsximport.ParseDebtors(file)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	result, err := h.store.Import(r.Context(), period, filepath.Base(header.Filename), items)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func parseDebtorPeriod(value string) (model.Date, error) {
	parsed, err := time.Parse("2006-01", value)
	if err != nil {
		return model.Date{}, err
	}
	return model.Date{Time: parsed}, nil
}
