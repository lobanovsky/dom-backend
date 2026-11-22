package xlsximport

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/mishramadhav/xls"

	"dom-backend/internal/model"
)

var um31Location = time.FixedZone("MSK", 3*60*60)

// ParseUM31 читает старый бинарный Excel УМ-31RTU. Заголовки находятся на второй строке.
func ParseUM31(r io.ReadSeeker) ([]model.UM31ReadingImportRow, []model.ImportRowError, error) {
	wb, err := xls.OpenReader(r, "utf-8")
	if err != nil {
		return nil, nil, fmt.Errorf("not a valid xls file: %w", err)
	}
	if wb.NumSheets() == 0 {
		return nil, nil, fmt.Errorf("workbook has no sheets")
	}
	data := wb.ReadAllCells(10000)
	if len(data) < 3 {
		return nil, nil, fmt.Errorf("file has no data rows")
	}
	col := make(map[string]int)
	for i, value := range data[1] {
		key := normalizeUM31Header(value)
		if key != "" {
			col[key] = i
		}
	}
	wanted := []string{"серномер", "датавремя", "a+0квт*ч", "a+1квт*ч", "a+2квт*ч", "a+3квт*ч"}
	indexes := make([]int, len(wanted))
	for i, name := range wanted {
		idx, ok := col[name]
		if !ok {
			return nil, nil, fmt.Errorf("header row must contain column %q", name)
		}
		indexes[i] = idx
	}

	var rows []model.UM31ReadingImportRow
	var errs []model.ImportRowError
	seen := make(map[string]int)
	for i := 2; i < len(data); i++ {
		cells := data[i]
		cell := func(index int) string {
			if index >= len(cells) {
				return ""
			}
			return strings.TrimSpace(cells[index])
		}
		if blank(cells) {
			continue
		}
		row := model.UM31ReadingImportRow{Row: i + 1, Serial: normalizeSerial(cell(indexes[0]))}
		// Выгрузка УМ-31RTU может содержать строки без привязанного счётчика.
		// Они не являются показаниями для импорта и не должны блокировать файл.
		if row.Serial == "" {
			errs = append(errs, model.ImportRowError{Row: i + 1, Error: "Не указан серийный номер счётчика; строка пропущена."})
			continue
		}
		rowErr := error(nil)
		row.ReadingDate, rowErr = parseUM31Date(cell(indexes[1]))
		for t := range row.T {
			if rowErr != nil {
				break
			}
			text := cell(indexes[t+3])
			if text == "" {
				continue
			}
			value, e := parseReading(text)
			if e != nil {
				rowErr = fmt.Errorf("A+%d: %w", t+1, e)
				break
			}
			row.T[t] = &value
		}
		if rowErr == nil {
			text := cell(indexes[2])
			if text != "" {
				value, e := parseReading(text)
				if e != nil {
					rowErr = fmt.Errorf("A+0: %w", e)
				} else {
					row.Total = &value
				}
			}
			if row.Total == nil && row.T[0] == nil {
				rowErr = fmt.Errorf("A+0 or A+1 reading is required")
			}
		}
		if rowErr == nil {
			if prev, ok := seen[row.Serial]; ok {
				rowErr = fmt.Errorf("duplicate meter serial number (already in row %d)", prev)
			}
		}
		if rowErr != nil {
			errs = append(errs, model.ImportRowError{Row: i + 1, Error: rowErr.Error()})
			continue
		}
		seen[row.Serial] = row.Row
		rows = append(rows, row)
	}
	if len(rows) == 0 && len(errs) == 0 {
		return nil, nil, fmt.Errorf("file has no data rows")
	}
	return rows, errs, nil
}

func normalizeUM31Header(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer(" ", "", "\u00a0", "", ",", "", ".", "", "/", "").Replace(s)
	return s
}

func normalizeSerial(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, ".0") {
		s = strings.TrimSuffix(s, ".0")
	}
	return s
}

func parseUM31Date(s string) (model.Date, error) {
	for _, layout := range []string{"02.01.2006 15:04", "02.01.2006 15:04:05", "2.1.2006 15:04", "2.1.2006 15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, um31Location); err == nil {
			return model.Date{Time: t}, nil
		}
	}
	if value, err := strconv.ParseFloat(s, 64); err == nil {
		// Excel 97–2003 stores date/time as days from 1899-12-30.
		seconds := int64(value * 24 * 60 * 60)
		return model.Date{Time: time.Unix(-2209161600+seconds, 0).In(um31Location)}, nil
	}
	return model.Date{}, fmt.Errorf("date/time %q has an unsupported format", s)
}
