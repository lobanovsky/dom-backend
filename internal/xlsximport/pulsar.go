package xlsximport

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"dom-backend/internal/model"
)

var (
	pulsarDateRE      = regexp.MustCompile(`(?i)(?:на\s+)?(\d{1,2}\.\d{1,2}\.\d{4})\s*$`)
	pulsarSerialRE    = regexp.MustCompile(`№\s*([0-9]+)`)
	pulsarApartmentRE = regexp.MustCompile(`(?i)^квартира\s*№?\s*([0-9]+(?:[-/][0-9]+)?)$`)
)

// ParsePulsar читает выгрузку Пульсара: дата находится в строке отчёта,
// ниже таблица с колонками «Объект», «Счётчик», «Показание».
func ParsePulsar(r io.Reader) (model.Date, []model.PulsarReadingImportRow, []model.ImportRowError, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return model.Date{}, nil, nil, fmt.Errorf("not a valid xlsx file: %w", err)
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return model.Date{}, nil, nil, fmt.Errorf("workbook has no sheets")
	}
	data, err := f.GetRows(sheets[0], excelize.Options{RawCellValue: true})
	if err != nil {
		return model.Date{}, nil, nil, fmt.Errorf("read sheet: %w", err)
	}
	date, headerRow, columns, err := pulsarHeader(data)
	if err != nil {
		return model.Date{}, nil, nil, err
	}

	var rows []model.PulsarReadingImportRow
	var issues []model.ImportRowError
	seen := make(map[string]int)
	for i := headerRow + 1; i < len(data); i++ {
		cells := data[i]
		if blank(cells) {
			continue
		}
		cell := func(index int) string {
			if index >= len(cells) {
				return ""
			}
			return strings.Join(strings.Fields(cells[index]), " ")
		}
		premises, err := parsePulsarPremises(cell(columns[0]))
		if err != nil {
			issues = append(issues, model.ImportRowError{Row: i + 1, Error: err.Error()})
			continue
		}
		resource, serial, err := parsePulsarMeter(cell(columns[1]))
		if err != nil {
			issues = append(issues, model.ImportRowError{Row: i + 1, Serial: serial, Error: err.Error()})
			continue
		}
		row := model.PulsarReadingImportRow{Row: i + 1, Premises: premises, Resource: resource, Serial: serial}
		readingText := cell(columns[2])
		if readingText != "" {
			value, err := parseReading(readingText)
			if err != nil {
				message := "Показание не является корректным числом."
				if strings.Contains(err.Error(), "must not be negative") {
					message = "Показание не может быть отрицательным."
				}
				issues = append(issues, model.ImportRowError{Row: i + 1, Serial: serial, Error: message})
				continue
			}
			row.Reading = &value
		}
		key := resource + ":" + serial
		if previous, ok := seen[key]; ok {
			issues = append(issues, model.ImportRowError{Row: i + 1, Serial: serial, Error: fmt.Sprintf("Повторный счётчик: уже указан в строке %d; строка пропущена.", previous)})
			continue
		}
		seen[key] = row.Row
		rows = append(rows, row)
	}
	if len(rows) == 0 && len(issues) == 0 {
		return model.Date{}, nil, nil, fmt.Errorf("file has no data rows")
	}
	return date, rows, issues, nil
}

func pulsarHeader(data [][]string) (model.Date, int, [3]int, error) {
	var date model.Date
	headerRow := -1
	columns := [3]int{-1, -1, -1}
	for i, row := range data {
		for _, cell := range row {
			if match := pulsarDateRE.FindStringSubmatch(strings.TrimSpace(cell)); len(match) == 2 {
				parsed, err := time.Parse("02.01.2006", match[1])
				if err == nil {
					date = model.Date{Time: parsed}
				}
			}
		}
		found := [3]int{-1, -1, -1}
		for col, cell := range row {
			switch strings.ToLower(strings.TrimSpace(cell)) {
			case "объект":
				found[0] = col
			case "счётчик", "счетчик":
				found[1] = col
			case "показание":
				found[2] = col
			}
		}
		if found[0] >= 0 && found[1] >= 0 && found[2] >= 0 {
			headerRow, columns = i, found
			break
		}
	}
	if date.IsZero() {
		return model.Date{}, 0, columns, fmt.Errorf("В заголовке отчёта не найдена дата в формате ДД.ММ.ГГГГ.")
	}
	if headerRow < 0 {
		return model.Date{}, 0, columns, fmt.Errorf("В файле не найдена строка заголовков «Объект», «Счётчик», «Показание».")
	}
	return date, headerRow, columns, nil
}

func parsePulsarPremises(value string) (string, error) {
	value = strings.TrimSpace(value)
	if match := pulsarApartmentRE.FindStringSubmatch(value); len(match) == 2 {
		return match[1], nil
	}
	if value != "" && strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
		return value, nil
	}
	return "", fmt.Errorf("Не удалось определить номер квартиры в колонке «Объект».")
}

func parsePulsarMeter(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	match := pulsarSerialRE.FindStringSubmatch(value)
	if len(match) != 2 {
		return "", "", fmt.Errorf("В колонке «Счётчик» не найден номер после знака №.")
	}
	model := strings.ToUpper(value)
	switch {
	case strings.HasPrefix(model, "СГИ"):
		return "hot_water", match[1], nil
	case strings.HasPrefix(model, "СХИ"):
		return "cold_water", match[1], nil
	default:
		return "", match[1], fmt.Errorf("Модель счётчика не относится к ХВС или ГВС; строка пропущена.")
	}
}
