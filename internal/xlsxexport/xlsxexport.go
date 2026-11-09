// Package xlsxexport собирает xlsx-файл с одной таблицей (выгрузка списков в Excel).
package xlsxexport

import (
	"bytes"
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"
)

// Column — колонка таблицы. Значения ячеек: string, float64 (деньги), time.Time (дата) или nil (пусто).
type Column struct {
	Title string
	Width float64
	Kind  Kind
}

// Kind — вид значений колонки: определяет числовой формат ячеек.
type Kind int

const (
	Text Kind = iota
	Money
	Date
)

// Build возвращает xlsx: заголовок (жирный, закреплён, с автофильтром) и строки rows.
type Sheet struct {
	Name    string
	Columns []Column
	Rows    [][]any
}

func Build(sheet string, cols []Column, rows [][]any) ([]byte, error) {
	return BuildSheets([]Sheet{{Name: sheet, Columns: cols, Rows: rows}})
}

// BuildSheets создаёт книгу с отдельной таблицей, фильтром и закреплением на каждом листе.
func BuildSheets(sheets []Sheet) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	for i, s := range sheets {
		if i == 0 {
			if err := f.SetSheetName("Sheet1", s.Name); err != nil {
				return nil, err
			}
		} else if _, err := f.NewSheet(s.Name); err != nil {
			return nil, err
		}
		if err := fillSheet(f, s.Name, s.Columns, s.Rows); err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fillSheet(f *excelize.File, sheet string, cols []Column, rows [][]any) error {
	head, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"#E8ECF1"}, Pattern: 1}})
	if err != nil {
		return err
	}
	money, err := f.NewStyle(&excelize.Style{NumFmt: 4}) // #,##0.00
	if err != nil {
		return err
	}
	date, err := f.NewStyle(&excelize.Style{CustomNumFmt: strPtr("dd.mm.yyyy")})
	if err != nil {
		return err
	}
	titles := make([]any, len(cols))
	for i, c := range cols {
		titles[i] = c.Title
		name, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(sheet, name, name, c.Width); err != nil {
			return err
		}
		switch c.Kind {
		case Money:
			err = f.SetColStyle(sheet, name, money)
		case Date:
			err = f.SetColStyle(sheet, name, date)
		}
		if err != nil {
			return err
		}
	}
	if err := f.SetSheetRow(sheet, "A1", &titles); err != nil {
		return err
	}
	last, _ := excelize.CoordinatesToCellName(len(cols), 1)
	if err := f.SetCellStyle(sheet, "A1", last, head); err != nil {
		return err
	}
	for i, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := f.SetSheetRow(sheet, cell, &row); err != nil {
			return fmt.Errorf("row %d: %w", i+1, err)
		}
	}
	if err := f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	if len(rows) > 0 {
		end, _ := excelize.CoordinatesToCellName(len(cols), len(rows)+1)
		if err := f.AutoFilter(sheet, "A1:"+end, nil); err != nil {
			return err
		}
	}
	return nil
}

func strPtr(s string) *string { return &s }

// Day обнуляет время у даты: в ячейке остаётся только день.
func Day(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }
