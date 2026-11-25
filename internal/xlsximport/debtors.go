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

// ParseDebtors reads the Domovladelets report using its FLS and end-of-period debt columns.
func ParseDebtors(r io.Reader) ([]model.DebtorImportRow, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть Excel-файл: %w", err)
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("в файле нет листов")
	}
	rows, err := f.GetRows(sheets[0], excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать лист: %w", err)
	}
	header, accountCol, debtCol := -1, -1, -1
	for i, row := range rows {
		for j, cell := range row {
			n := strings.ToLower(strings.Join(strings.Fields(cell), " "))
			switch n {
			case "флс", "лицевой счёт", "лицевой счет":
				accountCol = j
			case "долг на конец":
				debtCol = j
			}
		}
		if accountCol >= 0 && debtCol >= 0 {
			header = i
			break
		}
	}
	if header < 0 {
		return nil, fmt.Errorf("не найдены столбцы «ФЛС» и «Долг на конец»")
	}
	result := make([]model.DebtorImportRow, 0, len(rows)-header-1)
	seen := make(map[string]bool)
	for i := header + 1; i < len(rows); i++ {
		row := rows[i]
		if accountCol >= len(row) || debtCol >= len(row) {
			continue
		}
		cellName, err := excelize.CoordinatesToCellName(accountCol+1, i+1)
		if err != nil {
			return nil, fmt.Errorf("строка %d: не удалось прочитать номер лицевого счёта", i+1)
		}
		accountValue, err := f.GetCellValue(sheets[0], cellName)
		if err != nil {
			return nil, fmt.Errorf("строка %d: не удалось прочитать номер лицевого счёта", i+1)
		}
		account := strings.TrimSpace(accountValue)
		if account == "" {
			continue
		}
		debtText := strings.TrimSpace(strings.ReplaceAll(row[debtCol], ",", "."))
		debt, err := strconv.ParseFloat(debtText, 64)
		if err != nil || math.IsNaN(debt) || math.IsInf(debt, 0) {
			return nil, fmt.Errorf("строка %d: некорректный долг", i+1)
		}
		if seen[account] {
			return nil, fmt.Errorf("строка %d: лицевой счёт повторяется", i+1)
		}
		seen[account] = true
		if debt <= 0 {
			continue
		}
		result = append(result, model.DebtorImportRow{AccountNumber: account, Debt: debt})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("в файле не найдено положительной задолженности")
	}
	return result, nil
}
