package httpapi

import (
	"context"
	"dom-backend/internal/model"
	"dom-backend/internal/xlsxexport"
	"fmt"
	"net/http"
	"strconv"
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
	cols := []xlsxexport.Column{{Title: "Тип помещения", Width: 24}, {Title: "Номер помещения", Width: 20}, {Title: "Номер счётчика", Width: 26}, {Title: "Показания", Width: 20}}
	sheets := make([]xlsxexport.Sheet, 5)
	for i, name := range []string{"ХВС", "ГВС", "Т1", "Т2", "Т3"} {
		sheets[i] = xlsxexport.Sheet{Name: name, Columns: cols}
	}
	appendReading := func(index int, item model.MeterReadingExport, value float64) {
		sheets[index].Rows = append(sheets[index].Rows, []any{item.PremisesKind, item.PremisesNumber, item.SerialNumber, value})
	}
	for _, item := range items {
		switch item.Resource {
		case "cold_water":
			appendReading(0, item, item.T1)
		case "hot_water":
			appendReading(1, item, item.T1)
		case "electricity":
			appendReading(2, item, item.T1)
			if item.T2 != nil {
				appendReading(3, item, *item.T2)
			}
			if item.T3 != nil {
				appendReading(4, item, *item.T3)
			}
		}
	}
	data, err := xlsxexport.BuildSheets(sheets)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="meter-readings-%s.xlsx"`, period.Format("2006-01")))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}
