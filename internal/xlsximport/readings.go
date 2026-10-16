package xlsximport

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"dom-backend/internal/model"
)

// ParseReadings читает первый лист книги с показаниями счётчиков одного ресурса. Колонки находятся по заголовку:
// «Квартира», сразу за ней номер счётчика, затем показания (у воды одно, у электричества Т1–Т3), —
// пустые колонки слева допускаются. Первая строка — заголовок. Ошибки данных возвращаются списком по строкам.
func ParseReadings(r io.Reader, resource string) ([]model.MeterReadingImportRow, []model.ImportRowError, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, nil, fmt.Errorf("not a valid xlsx file: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, nil, fmt.Errorf("workbook has no sheets")
	}
	// Исходные значения ячеек: отформатированное «30,381.731» не годится для разбора числа.
	data, err := f.GetRows(sheets[0], excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, nil, fmt.Errorf("read sheet: %w", err)
	}
	if len(data) == 0 {
		return nil, nil, fmt.Errorf("file has no data rows")
	}
	first := -1
	for i, c := range data[0] {
		if strings.EqualFold(strings.TrimSpace(c), "квартира") {
			first = i
			break
		}
	}
	if first < 0 {
		return nil, nil, fmt.Errorf(`header row must contain the column "Квартира"`)
	}
	tariffs := 1
	if resource == "electricity" {
		tariffs = 3
	}

	var (
		rows   []model.MeterReadingImportRow
		errs   []model.ImportRowError
		serial = map[string]int{}
	)
	for i, cells := range data {
		if i == 0 || blank(cells) {
			continue
		}
		row, err := parseReadingRow(i+1, cells, first, tariffs)
		if err == nil {
			if prev, ok := serial[row.Serial]; ok {
				err = fmt.Errorf("duplicate meter number %q (already in row %d)", row.Serial, prev)
			}
		}
		if err != nil {
			errs = append(errs, model.ImportRowError{Row: i + 1, Error: err.Error()})
			continue
		}
		serial[row.Serial] = row.Row
		rows = append(rows, row)
	}
	if len(rows) == 0 && len(errs) == 0 {
		return nil, nil, fmt.Errorf("file has no data rows")
	}
	return rows, errs, nil
}

func parseReadingRow(n int, cells []string, first, tariffs int) (model.MeterReadingImportRow, error) {
	cell := func(i int) string {
		if i >= len(cells) {
			return ""
		}
		return strings.Join(strings.Fields(cells[i]), " ")
	}
	row := model.MeterReadingImportRow{Row: n, Premises: cell(first), Serial: cell(first + 1)}
	if row.Premises == "" {
		return row, fmt.Errorf("premises number is required")
	}
	if row.Serial == "" {
		return row, fmt.Errorf("meter number is required")
	}
	for t := 0; t < tariffs; t++ {
		s := cell(first + 2 + t)
		if s == "" {
			continue
		}
		if t > 0 && row.T[t-1] == nil {
			return row, fmt.Errorf("t%d: tariffs must be filled in order", t+1)
		}
		v, err := parseReading(s)
		if err != nil {
			return row, fmt.Errorf("t%d: %w", t+1, err)
		}
		row.T[t] = &v
	}
	if row.T[0] == nil {
		return row, fmt.Errorf("t1: reading is required")
	}
	return row, nil
}

// parseReading разбирает показание: число или текст вида «30,381.731» (запятая — разделитель тысяч,
// если есть точка) и «633,99» (запятая — десятичный разделитель). Округляется до трёх знаков после запятой.
func parseReading(s string) (float64, error) {
	s = strings.NewReplacer(" ", "", " ", "").Replace(s)
	if strings.Contains(s, ".") {
		s = strings.ReplaceAll(s, ",", "")
	} else {
		s = strings.ReplaceAll(s, ",", ".")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	if v < 0 {
		return 0, fmt.Errorf("must not be negative")
	}
	// Ячейки бывают результатом формулы и хранят длинный хвост знаков, а на экране показаны три: берём их.
	return math.Round(v*1000) / 1000, nil
}
