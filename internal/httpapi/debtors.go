package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dom-backend/internal/model"
	"dom-backend/internal/xlsxexport"
	"dom-backend/internal/xlsximport"
)

type DebtorsStore interface {
	Import(context.Context, model.Date, string, []model.DebtorImportRow) (model.DebtorImportResult, error)
	ListReports(context.Context) ([]model.DebtorReport, error)
	DeleteReport(context.Context, model.Date) error
	List(context.Context, model.Date) ([]model.DebtorReportRow, error)
}

type debtorHandlers struct{ store DebtorsStore }

func (h debtorHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/debtors/reports", h.reports)
	mux.HandleFunc("DELETE /api/v1/debtors/reports/{period}", h.deleteReport)
	mux.HandleFunc("GET /api/v1/debtors", h.list)
	mux.HandleFunc("GET /api/v1/debtors/export", h.export)
	mux.HandleFunc("POST /api/v1/debtors/import", h.importFile)
}

func (h debtorHandlers) deleteReport(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		writeError(w, http.StatusBadRequest, "unexpected query parameters")
		return
	}
	period, err := parseDebtorPeriod(r.PathValue("period"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "period must have format YYYY-MM")
		return
	}
	if err := h.store.DeleteReport(r.Context(), period); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h debtorHandlers) export(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	period, err := parseDebtorPeriod(q.Get("period"))
	if err != nil || len(q) == 0 || len(q["period"]) != 1 || len(q["category"]) > 1 || len(q["q"]) > 1 || len(q) != 1+boolInt(len(q["category"]) == 1)+boolInt(len(q["q"]) == 1) {
		writeError(w, http.StatusBadRequest, "period=YYYY-MM, optional category and q are required")
		return
	}
	category, search := q.Get("category"), strings.ToLower(strings.TrimSpace(q.Get("q")))
	validCategories := map[string]bool{"": true, "all": true, "other": true, "utilities:apartment": true, "utilities:parking_space": true, "capital_repair:apartment": true, "capital_repair:parking_space": true}
	if !validCategories[category] {
		writeError(w, http.StatusBadRequest, "category: unknown value")
		return
	}
	items, err := h.store.List(r.Context(), period)
	if err != nil {
		writeErr(w, err)
		return
	}
	cols := []xlsxexport.Column{{Title: "Лицевой счёт", Width: 18}, {Title: "Номер помещения", Width: 22}, {Title: "Тип счёта", Width: 28}, {Title: "Собственник", Width: 38}, {Title: "Телефон", Width: 24}, {Title: "Email", Width: 36}, {Title: "Долг", Width: 18, Kind: xlsxexport.Money}}
	rows := make([][]any, 0, len(items))
	for _, item := range items {
		if category != "" && category != "all" && debtorCategory(item) != category {
			continue
		}
		if search != "" && !debtorContains(item, search) {
			continue
		}
		rows = append(rows, []any{item.AccountNumber, debtorString(item.PremisesNo), debtorCategoryLabel(item), strings.Join(item.HolderNames, ", "), formatDebtorPhones(item.Phones), strings.Join(item.Emails, ", "), item.Debt})
	}
	data, err := xlsxexport.Build("Должники", cols, rows)
	if err != nil {
		writeErr(w, err)
		return
	}
	name := "должники-" + period.Format("2006-01") + ".xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

func debtorCategory(row model.DebtorReportRow) string {
	kind := debtorString(row.PremisesKind)
	if kind == "apartment" || kind == "parking_space" {
		return debtorString(row.Purpose) + ":" + kind
	}
	return "other"
}

func debtorCategoryLabel(row model.DebtorReportRow) string {
	kind := debtorString(row.PremisesKind)
	switch kind {
	case "apartment":
		kind = "Квартира"
	case "parking_space":
		kind = "Машиноместо"
	case "commercial":
		kind = "Коммерческое помещение"
	case "":
		kind = "Помещение не найдено"
	}
	purpose := "ЖКУ"
	if debtorString(row.Purpose) == "capital_repair" {
		purpose = "капремонт"
	}
	return kind + " · " + purpose
}

func debtorContains(row model.DebtorReportRow, search string) bool {
	values := []string{row.AccountNumber, debtorString(row.PremisesNo), strings.Join(row.HolderNames, " "), strings.Join(row.Emails, " "), strings.Join(row.Phones, " ")}
	for _, item := range values {
		if strings.Contains(strings.ToLower(item), search) {
			return true
		}
	}
	return false
}

func formatDebtorPhones(phones []string) string {
	formatted := make([]string, 0, len(phones))
	for _, phone := range phones {
		digits := make([]rune, 0, len(phone))
		for _, char := range phone {
			if char >= '0' && char <= '9' {
				digits = append(digits, char)
			}
		}
		if len(digits) == 11 && digits[0] == '7' {
			phone = fmt.Sprintf("+7 (%s) %s-%s-%s", string(digits[1:4]), string(digits[4:7]), string(digits[7:9]), string(digits[9:11]))
		}
		formatted = append(formatted, phone)
	}
	return strings.Join(formatted, ", ")
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func debtorString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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
