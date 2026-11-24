package httpapi

import (
	"archive/zip"
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
func TestMeterExportArchive(t *testing.T) {
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
	body := w.Body.Bytes()
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 5 {
		t.Fatalf("got %d files, want 5", len(archive.File))
	}
	for sheet, want := range map[string][]string{
		"ХВС": {"Квартира", "001", "000123", "0"},
		"ГВС": nil,
		"Т1":  {"Машиноместо", "7", "009", "100.125"},
		"Т2":  {"Машиноместо", "7", "009", "12.345"},
		"Т3":  {"Машиноместо", "7", "009", "0"},
	} {
		entry, err := archive.Open(sheet + "-2026-11.xlsx")
		if err != nil {
			t.Fatal(err)
		}
		book, err := excelize.OpenReader(entry)
		entry.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(book.GetSheetList(), []string{sheet}) {
			t.Errorf("unexpected sheets: %v", book.GetSheetList())
		}
		rows, err := book.GetRows(sheet)
		book.Close()
		expected := [][]string{{"Тип помещения", "Номер помещения", "Номер счётчика", "Показания"}}
		if want != nil {
			expected = append(expected, want)
		}
		if err != nil || !reflect.DeepEqual(rows, expected) {
			t.Errorf("%s: got %v, want %v (%v)", sheet, rows, expected, err)
		}
	}
}
