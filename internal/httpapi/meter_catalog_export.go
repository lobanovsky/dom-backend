package httpapi

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"dom-backend/internal/model"
	"dom-backend/internal/xlsxexport"
)

const maxMeterCatalogExport = 100000

type meterCatalogExportStore interface {
	List(context.Context, model.MeterFilter, int, int) ([]model.Meter, error)
}

// electricityPaperForm выгружает действующие квартирные электросчётчики
// в бланк для записи показаний на бумаге.
func (h meterCatalogExportHandlers) electricityPaperForm(w http.ResponseWriter, r *http.Request) {
	buildingID, err := strconv.ParseInt(r.URL.Query().Get("building_id"), 10, 64)
	if err != nil || buildingID <= 0 || len(r.URL.Query()) != 1 {
		writeError(w, http.StatusUnprocessableEntity, "building_id: must be a positive integer")
		return
	}
	scope, resource, active := "individual", "electricity", true
	items, err := h.s.List(r.Context(), model.MeterFilter{Scope: &scope, BuildingID: &buildingID, Resource: &resource, Active: &active}, maxMeterCatalogExport+1, 0)
	if err != nil {
		writeErr(w, err)
		return
	}
	rows := make([]model.Meter, 0, len(items))
	for _, item := range items {
		if item.PremisesKind != nil && *item.PremisesKind == "apartment" {
			rows = append(rows, item)
		}
	}
	if len(rows) > maxMeterCatalogExport {
		writeError(w, http.StatusUnprocessableEntity, "too many meters for export")
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		fi, fj := rows[i].PremisesFloor, rows[j].PremisesFloor
		if fi == nil || fj == nil {
			if fi == nil && fj != nil {
				return false
			}
			if fi != nil && fj == nil {
				return true
			}
		} else if *fi != *fj {
			return *fi < *fj
		}
		ni, nj := "", ""
		if rows[i].PremisesNumber != nil {
			ni = *rows[i].PremisesNumber
		}
		if rows[j].PremisesNumber != nil {
			nj = *rows[j].PremisesNumber
		}
		if ni != nj {
			if ai, err := strconv.ParseInt(ni, 10, 64); err == nil {
				if aj, err := strconv.ParseInt(nj, 10, 64); err == nil {
					return ai < aj
				}
			}
			return strings.ToLower(ni) < strings.ToLower(nj)
		}
		return rows[i].SerialNumber < rows[j].SerialNumber
	})
	columns := []xlsxexport.Column{
		{Title: "Этаж", Width: 10}, {Title: "Квартира", Width: 14}, {Title: "Номер электросчётчика", Width: 24},
		{Title: "Предыдущее показание Т1", Width: 22}, {Title: "Новое показание Т1", Width: 20},
		{Title: "Предыдущее показание Т2", Width: 22}, {Title: "Новое показание Т2", Width: 20},
		{Title: "Предыдущее показание Т3", Width: 22}, {Title: "Новое показание Т3", Width: 20},
	}
	dataRows := make([][]any, 0, len(rows))
	for _, meter := range rows {
		floor, premise := any(nil), any("")
		if meter.PremisesFloor != nil {
			floor = *meter.PremisesFloor
		}
		if meter.PremisesNumber != nil {
			premise = *meter.PremisesNumber
		}
		t1, t2, t3 := any(meter.InitialT1), any(nil), any(nil)
		if meter.LastT1 != nil {
			t1 = *meter.LastT1
		}
		if meter.Tariffs >= 2 {
			t2 = any(meter.InitialT2)
			if meter.LastT2 != nil {
				t2 = *meter.LastT2
			}
		}
		if meter.Tariffs >= 3 {
			t3 = any(meter.InitialT3)
			if meter.LastT3 != nil {
				t3 = *meter.LastT3
			}
		}
		dataRows = append(dataRows, []any{floor, premise, meter.SerialNumber, t1, nil, t2, nil, t3, nil})
	}
	data, err := xlsxexport.Build("Показания электросчётчиков", columns, dataRows)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="electricity-readings-paper-form.xlsx"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
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
		{Title: "Модель", Width: 22}, {Title: "Показания передавать вручную", Width: 28},
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
			yesNo(m.ManualReadingRequired), meterDate(m.InstalledAt), optionalMeterDate(m.RemovedAt), optionalMeterDate(m.LastReadingDate),
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
