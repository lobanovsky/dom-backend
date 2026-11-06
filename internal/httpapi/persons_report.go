package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"dom-backend/internal/model"
	"dom-backend/internal/personsreport"
)

type PersonsReportStore interface {
	Build(ctx context.Context, day time.Time) (model.PersonsReport, error)
}

type personsReportHandlers struct{ s PersonsReportStore }

func (h personsReportHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/persons/properties-report", h.pdf)
}

// pdf отдаёт отчёт «физлица и их недвижимость» в PDF; необязательный параметр date (YYYY-MM-DD) — на какую дату, по умолчанию сегодня.
func (h personsReportHandlers) pdf(w http.ResponseWriter, r *http.Request) {
	day := time.Now()
	if v := r.URL.Query().Get("date"); v != "" {
		d, err := time.ParseInLocation("2006-01-02", v, time.Local)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid date, expected YYYY-MM-DD")
			return
		}
		day = d
	}
	rep, err := h.s.Build(r.Context(), day)
	if err != nil {
		writeErr(w, err)
		return
	}
	data, err := personsreport.Render(rep)
	if err != nil {
		writeErr(w, err)
		return
	}
	name := "Физлица и недвижимость " + rep.Day + ".pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}
