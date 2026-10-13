package xlsxexport

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestBuild(t *testing.T) {
	cols := []Column{{"Дата", 12, Date}, {"Кому", 30, Text}, {"Сумма", 14, Money}}
	data, err := Build("Платежи", cols, [][]any{
		{Day(time.Date(2026, 8, 3, 15, 4, 5, 0, time.UTC)), "ООО Ромашка", 1234.5},
		{Day(time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)), "", nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := f.GetRows("Платежи")
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows = %v, err = %v", rows, err)
	}
	if rows[0][0] != "Дата" || rows[1][1] != "ООО Ромашка" {
		t.Errorf("content: %v", rows)
	}
	if v, _ := f.GetCellValue("Платежи", "A2"); v != "03.08.2026" {
		t.Errorf("date cell = %q", v)
	}
	if v, _ := f.GetCellValue("Платежи", "C2"); v != "1,234.50" {
		t.Errorf("money cell = %q", v)
	}
}
