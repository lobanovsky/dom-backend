package httpapi

import (
	"bytes"
	"context"
	"dom-backend/internal/model"
	"github.com/xuri/excelize/v2"
	"net/http/httptest"
	"reflect"
	"testing"
)

type fakeMeterExport struct{ period model.Date }

func (f *fakeMeterExport) Export(_ context.Context, p model.Date) ([]model.MeterReadingExport, error) {
	f.period = p
	t2 := 12.345
	t3 := 0.0
	return []model.MeterReadingExport{{Resource: "cold_water", PremisesKind: "Квартира", PremisesNumber: "001", SerialNumber: "000123", T1: 0}, {Resource: "electricity", PremisesKind: "Машиноместо", PremisesNumber: "7", SerialNumber: "009", T1: 100.125, T2: &t2, T3: &t3}}, nil
}
func TestMeterExportWorkbook(t *testing.T) {
	f := &fakeMeterExport{}
	h := meterExportHandlers{f}
	for _, query := range []string{"", "?period=2026-13", "?period=2026-11-01", "?period=2026-11&x=1", "?period=2026-11&period=2026-12"} {
		w := httptest.NewRecorder()
		h.export(w, httptest.NewRequest("GET", "/api/v1/meter-readings/export"+query, nil))
		if w.Code != 422 {
			t.Fatalf("invalid query %s: %d", query, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.export(w, httptest.NewRequest("GET", "/api/v1/meter-readings/export?period=2026-11", nil))
	if w.Code != 200 || f.period.Format("2006-01-02") != "2026-11-01" {
		t.Fatalf("export: %d", w.Code)
	}
	book, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	if !reflect.DeepEqual(book.GetSheetList(), []string{"ХВС", "ГВС", "Т1", "Т2", "Т3"}) {
		t.Fatal(book.GetSheetList())
	}
	for cell, want := range map[string]string{"A1": "Тип помещения", "B1": "Номер помещения", "C1": "Номер счётчика", "D1": "Показания", "B2": "001", "C2": "000123", "D2": "0"} {
		got, err := book.GetCellValue("ХВС", cell)
		if err != nil || got != want {
			t.Errorf("%s: %q want %q (%v)", cell, got, want, err)
		}
	}
	for sheet, want := range map[string]string{"Т1": "100.125", "Т2": "12.345", "Т3": "0"} {
		got, err := book.GetCellValue(sheet, "D2")
		if err != nil || got != want {
			t.Errorf("%s: %q (%v)", sheet, got, err)
		}
	}
	rows, err := book.GetRows("ГВС")
	if err != nil || len(rows) != 1 {
		t.Fatalf("empty sheet: %+v %v", rows, err)
	}

}
