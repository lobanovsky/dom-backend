package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"dom-backend/internal/model"
	"dom-backend/internal/xlsxexport"
)

const maxMeterCatalogExport = 100000

type meterCatalogExportStore interface {
	List(context.Context, model.MeterFilter, int, int) ([]model.Meter, error)
}

type meterCatalogExportHandlers struct{ s meterCatalogExportStore }

func (h meterCatalogExportHandlers) export(w http.ResponseWriter, r *http.Request) {
	f, _, ok := meterFilter(w, r)
	if !ok {
		return
	}
	items, err := h.s.List(r.Context(), f, maxMeterCatalogExport+1, 0)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(items) > maxMeterCatalogExport {
		writeError(w, http.StatusUnprocessableEntity, "too many meters for export: narrow the filters to at most 100000")
		return
	}

	cols := []xlsxexport.Column{
		{Title: "Дом", Width: 34}, {Title: "Вид помещения", Width: 20}, {Title: "Номер помещения", Width: 18},
		{Title: "Ресурс", Width: 18}, {Title: "Номер счётчика", Width: 24}, {Title: "Название", Width: 24},
		{Title: "Модель", Width: 22}, {Title: "Сломан", Width: 12}, {Title: "Показания передавать вручную", Width: 28},
		{Title: "Введён в эксплуатацию", Width: 20, Kind: xlsxexport.Date}, {Title: "Окончание работы", Width: 18, Kind: xlsxexport.Date},
		{Title: "Последнее показание", Width: 18, Kind: xlsxexport.Date}, {Title: "Расчётный месяц", Width: 18, Kind: xlsxexport.Date},
		{Title: "Т1", Width: 14}, {Title: "Т2", Width: 14}, {Title: "Т3", Width: 14}, {Title: "Комментарий", Width: 40},
	}
	rows := make([][]any, 0, len(items))
	for _, m := range items {
		premisesKind, premisesNumber := "", ""
		if m.PremisesKind != nil {
			premisesKind = *m.PremisesKind
		}
		if m.PremisesNumber != nil {
			premisesNumber = *m.PremisesNumber
		}
		building := m.BuildingAddress
		if m.BuildingName != nil && *m.BuildingName != "" {
			building = *m.BuildingName + " — " + building
		}
		rows = append(rows, []any{
			building, premisesKind, premisesNumber, meterResourceName(m.Resource), m.SerialNumber, stringValue(m.Name), stringValue(m.Model),
			yesNo(m.Broken), yesNo(m.ManualReadingRequired), meterDate(m.InstalledAt), optionalMeterDate(m.RemovedAt), optionalMeterDate(m.LastReadingDate),
			optionalMeterDate(m.LastReadingPeriod), meterNumber(m.LastT1), meterNumber(m.LastT2), meterNumber(m.LastT3), stringValue(m.Comment),
		})
	}
	data, err := xlsxexport.Build("Счётчики", cols, rows)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="meters.xlsx"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

func meterResourceName(resource string) string {
	switch resource {
	case "cold_water":
		return "Холодная вода"
	case "hot_water":
		return "Горячая вода"
	case "electricity":
		return "Электроэнергия"
	default:
		return resource
	}
}

func meterDate(d model.Date) any { return xlsxexport.Day(d.Time) }

func optionalMeterDate(d *model.Date) any {
	if d == nil {
		return nil
	}
	return meterDate(*d)
}

func stringValue(s *string) any {
	if s == nil {
		return ""
	}
	return *s
}

func meterNumber(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func yesNo(value bool) string {
	if value {
		return "Да"
	}
	return "Нет"
}
