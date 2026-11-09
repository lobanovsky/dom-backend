package httpapi

import (
	"context"
	"dom-backend/internal/model"
	"dom-backend/internal/xlsxexport"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type MeterReadingExportStore interface {
	Export(context.Context, model.Date) ([]model.MeterReadingExport, error)
}

type meterExportHandlers struct{ s MeterReadingExportStore }

func (h meterExportHandlers) export(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	period, err := time.Parse("2006-01", q.Get("period"))
	if err != nil || period.Year() < 1 || len(q) != 1 || len(q["period"]) != 1 {
		writeError(w, 422, "period: must be YYYY-MM")
		return
	}
	items, err := h.s.Export(r.Context(), model.Date{Time: period})
	if err != nil {
		writeErr(w, err)
		return
	}
	rows := make([][]any, 0, len(items))
	for _, item := range items {
		var reading any = item.T1
		if item.T2 != nil || item.T3 != nil {
			parts := []string{"Т1: " + strconv.FormatFloat(item.T1, 'f', -1, 64)}
			if item.T2 != nil {
				parts = append(parts, "Т2: "+strconv.FormatFloat(*item.T2, 'f', -1, 64))
			}
			if item.T3 != nil {
				parts = append(parts, "Т3: "+strconv.FormatFloat(*item.T3, 'f', -1, 64))
			}
			reading = strings.Join(parts, "; ")
		}
		rows = append(rows, []any{item.PremisesKind, item.PremisesNumber, item.SerialNumber, reading})
	}
	data, err := xlsxexport.Build("Показания", []xlsxexport.Column{{Title: "Тип помещения", Width: 24}, {Title: "Номер помещения", Width: 20}, {Title: "Номер счётчика", Width: 26}, {Title: "Показания", Width: 45}}, rows)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="meter-readings-%s.xlsx"`, period.Format("2006-01")))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}
