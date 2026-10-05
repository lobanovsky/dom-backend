package xlsximport

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func workbook(t *testing.T, rows [][]any) *bytes.Reader {
	t.Helper()
	f := excelize.NewFile()
	for i, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow("Sheet1", cell, &row); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

var header = []any{"Номер", "Площадь", "Кадастр", "Фамилия", "Имя", "Отчество", "ЖКУ", "Кап.ремонт"}

func TestParse(t *testing.T) {
	rows, errs, err := Parse(workbook(t, [][]any{
		header,
		{"1", "97,2", "77:02:0021009:2346", " Федоров ", "Дмитрий", "Викторович", "0000001001", "0000500001"},
		{},
		{"Оф. 3", 51.2, "", "Кишмишян", "Арсен", "", "0000002003", "0000004164"},
	}))
	if err != nil || len(errs) != 0 {
		t.Fatalf("err = %v, errs = %v", err, errs)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if r := rows[0]; r.Row != 2 || r.Area != 97.2 || r.LastName != "Федоров" || r.UtilitiesAccount != "0000001001" {
		t.Errorf("row 0 = %+v", r)
	}
	if r := rows[1]; r.Row != 4 || r.Number != "Оф. 3" || r.Area != 51.2 || r.CadastralNumber != "" || r.MiddleName != "" {
		t.Errorf("row 1 = %+v", r)
	}
}

func TestParseRowErrors(t *testing.T) {
	_, errs, err := Parse(workbook(t, [][]any{
		header,
		{"1", "10", "k1", "А", "Б", "", "a1", "b1"},
		{"1", "10", "k2", "А", "Б", "", "a2", "b2"},     // повтор номера
		{"2", "0", "k3", "А", "Б", "", "a3", "b3"},      // площадь
		{"3", "10", "k1", "А", "Б", "", "a4", "b4"},     // повтор кадастра
		{"4", "10", "k5", "", "Б", "", "a5", "b5"},      // нет фамилии
		{"5", "10", "k6", "А", "Б", "", "a1", "b6"},     // повтор счёта
		{"6", "10", "k7", "А", "Б", "", "same", "same"}, // счета совпадают
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"duplicate number", "area", "duplicate cadastral", "last_name is required", "duplicate account", "must differ"}
	if len(errs) != len(want) {
		t.Fatalf("errs = %v", errs)
	}
	for i, w := range want {
		if !strings.Contains(errs[i].Error, w) || errs[i].Row != i+3 {
			t.Errorf("errs[%d] = %+v, want row %d containing %q", i, errs[i], i+3, w)
		}
	}
}

func TestParseNotXLSX(t *testing.T) {
	if _, _, err := Parse(strings.NewReader("not a zip")); err == nil {
		t.Fatal("expected error")
	}
}
