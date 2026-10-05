// Package xlsximport разбирает xlsx-файл со списком помещений, собственников и лицевых счетов.
package xlsximport

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"dom-backend/internal/model"
)

// Колонки фиксированы по порядку; первая строка — заголовок и пропускается.
const (
	colNumber = iota
	colArea
	colCadastral
	colLastName
	colFirstName
	colMiddleName
	colUtilities
	colCapitalRepair
	columns
)

// Parse читает первый лист книги. Ошибки данных возвращаются списком по строкам,
// чтобы пользователь исправил их все за один раз; err — только если файл нечитаем.
func Parse(r io.Reader) ([]model.ImportRow, []model.ImportRowError, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, nil, fmt.Errorf("not a valid xlsx file: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, nil, fmt.Errorf("workbook has no sheets")
	}
	data, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, nil, fmt.Errorf("read sheet: %w", err)
	}

	var (
		rows []model.ImportRow
		errs []model.ImportRowError
		seen = newUniques()
	)
	for i, cells := range data {
		if i == 0 || blank(cells) {
			continue
		}
		row, err := parseRow(i+1, cells)
		if err == nil {
			err = seen.check(row)
		}
		if err != nil {
			errs = append(errs, model.ImportRowError{Row: i + 1, Error: err.Error()})
			continue
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 && len(errs) == 0 {
		return nil, nil, fmt.Errorf("file has no data rows")
	}
	return rows, errs, nil
}

func parseRow(n int, cells []string) (model.ImportRow, error) {
	cell := func(i int) string {
		if i >= len(cells) {
			return ""
		}
		return strings.Join(strings.Fields(cells[i]), " ")
	}
	row := model.ImportRow{
		Row:                  n,
		Number:               cell(colNumber),
		CadastralNumber:      cell(colCadastral),
		LastName:             cell(colLastName),
		FirstName:            cell(colFirstName),
		MiddleName:           cell(colMiddleName),
		UtilitiesAccount:     cell(colUtilities),
		CapitalRepairAccount: cell(colCapitalRepair),
	}
	for _, c := range []struct{ name, v string }{
		{"number", row.Number},
		{"last_name", row.LastName},
		{"first_name", row.FirstName},
		{"utilities_account", row.UtilitiesAccount},
		{"capital_repair_account", row.CapitalRepairAccount},
	} {
		if c.v == "" {
			return row, fmt.Errorf("%s is required", c.name)
		}
	}
	if row.UtilitiesAccount == row.CapitalRepairAccount {
		return row, fmt.Errorf("utilities and capital repair accounts must differ")
	}
	area, err := strconv.ParseFloat(strings.ReplaceAll(cell(colArea), ",", "."), 64)
	if err != nil || area <= 0 {
		return row, fmt.Errorf("area: %q is not a positive number", cell(colArea))
	}
	row.Area = area
	return row, nil
}

func blank(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// uniques ловит повторы внутри файла: номер помещения, кадастровый номер, лицевые счета.
type uniques struct{ numbers, cadastral, accounts map[string]int }

func newUniques() *uniques {
	return &uniques{map[string]int{}, map[string]int{}, map[string]int{}}
}

func (u *uniques) check(r model.ImportRow) error {
	if err := u.add(u.numbers, r.Number, r.Row, "number"); err != nil {
		return err
	}
	if r.CadastralNumber != "" {
		if err := u.add(u.cadastral, r.CadastralNumber, r.Row, "cadastral number"); err != nil {
			return err
		}
	}
	for _, acc := range []string{r.UtilitiesAccount, r.CapitalRepairAccount} {
		if err := u.add(u.accounts, acc, r.Row, "account"); err != nil {
			return err
		}
	}
	return nil
}

func (u *uniques) add(m map[string]int, v string, row int, what string) error {
	if first, ok := m[v]; ok {
		return fmt.Errorf("duplicate %s %q (already in row %d)", what, v, first)
	}
	m[v] = row
	return nil
}
