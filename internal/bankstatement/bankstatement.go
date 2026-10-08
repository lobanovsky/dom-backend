// Package bankstatement разбирает банковскую выписку СберБизнес в формате xlsx.
//
// Выписка: шапка (наш счёт, период), таблица операций и итог внизу. Колонки находятся по подписям заголовков,
// а не по буквам. В строке операции блок дебета и блок кредита имеют вид «счёт\nИНН\nназвание»; сумма стоит
// в колонке «Сумма по дебету» (списание с нашего счёта) или «Сумма по кредиту» (поступление).
package bankstatement

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"dom-backend/internal/model"
)

var (
	accountRe = regexp.MustCompile(`(?:^|\D)(\d{20})(?:\D|$)`)
	dateRu    = regexp.MustCompile(`(\d{1,2})\s+([а-яА-Я]+)\s+(\d{4})`)
	bikRe     = regexp.MustCompile(`БИК\s*(\d{9})\s*(.*)`)
	months    = map[string]time.Month{
		"января": 1, "февраля": 2, "марта": 3, "апреля": 4, "мая": 5, "июня": 6,
		"июля": 7, "августа": 8, "сентября": 9, "октября": 10, "ноября": 11, "декабря": 12,
	}
)

// columns — позиции колонок (с 1) по заголовкам.
type columns struct {
	date, debitBlock, creditBlock, debitSum, creditSum, doc, vo, bank, purpose int
}

// Parse разбирает файл. Ошибки данных собираются по строкам; err — файл в целом непригоден.
func Parse(data []byte) (*model.ParsedStatement, []model.ImportRowError, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("not a valid xlsx file")
	}
	defer f.Close()

	sheet, headerRow, rows := findSheet(f)
	if sheet == "" {
		return nil, nil, fmt.Errorf(`statement header "Дата проводки" not found: not a СберБизнес statement`)
	}
	cols, err := findColumns(rows, headerRow)
	if err != nil {
		return nil, nil, err
	}

	st := &model.ParsedStatement{}
	st.Account = findAccount(rows, headerRow, sheet)
	if st.Account == "" {
		return nil, nil, fmt.Errorf("our account number is not found in the statement header")
	}
	st.PeriodFrom, st.PeriodTo = findPeriod(rows, headerRow)

	raw := func(col, row int) string {
		name, _ := excelize.CoordinatesToCellName(col, row)
		v, _ := f.GetCellValue(sheet, name, excelize.Options{RawCellValue: true})
		return strings.TrimSpace(v)
	}

	var errs []model.ImportRowError
	seen := map[string]int{}
	unreadable := 0 // строки с суммой, но с датой, которую не удалось прочитать
	for r := headerRow + 2; r <= len(rows); r++ {
		at, ok := cellDate(raw(cols.date, r))
		if !ok {
			// пустая строка, итог или подпись; но строка с суммой и непонятной датой — потерянная операция
			if raw(cols.date, r) != "" && (raw(cols.debitSum, r) != "" || raw(cols.creditSum, r) != "") {
				unreadable++
			}
			continue
		}
		op, err := parseOperation(st.Account, cols, raw, r, at)
		if err != nil {
			errs = append(errs, model.ImportRowError{Row: r, Error: err.Error()})
			continue
		}
		op.DedupKey = dedupKey(op, seen)
		st.Operations = append(st.Operations, op)
		if op.Outgoing {
			st.DebitCount++
			st.DebitTotal += op.Amount
		} else {
			st.CreditCount++
			st.CreditTotal += op.Amount
		}
	}
	if len(errs) > 0 {
		return st, errs, nil
	}
	if len(st.Operations) == 0 {
		return nil, nil, fmt.Errorf("statement has no operations")
	}
	if err := checkFooter(st, rows, raw, headerRow, unreadable); err != nil {
		return nil, nil, err
	}
	if st.PeriodFrom == nil || st.PeriodTo == nil {
		first, last := st.Operations[0].At, st.Operations[0].At
		for _, op := range st.Operations {
			if op.At.Before(first) {
				first = op.At
			}
			if op.At.After(last) {
				last = op.At
			}
		}
		st.PeriodFrom, st.PeriodTo = &model.Date{Time: day(first)}, &model.Date{Time: day(last)}
	}
	return st, nil, nil
}

func day(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }

// findSheet ищет лист с заголовком таблицы и возвращает строки листа (для поиска подписей).
func findSheet(f *excelize.File) (sheet string, headerRow int, rows [][]string) {
	for _, name := range f.GetSheetList() {
		rs, err := f.GetRows(name)
		if err != nil {
			continue
		}
		for i, row := range rs {
			for _, c := range row {
				if label(c) == "Дата проводки" {
					return name, i + 1, rs
				}
			}
		}
	}
	return "", 0, nil
}

// label приводит подпись к виду без переводов строк и лишних пробелов.
func label(s string) string { return strings.Join(strings.Fields(s), " ") }

func findColumns(rows [][]string, headerRow int) (columns, error) {
	var c columns
	find := func(row []string, want string) int {
		for i, v := range row {
			if label(v) == want {
				return i + 1
			}
		}
		return 0
	}
	head := rows[headerRow-1]
	c.date = find(head, "Дата проводки")
	c.debitSum = find(head, "Сумма по дебету")
	c.creditSum = find(head, "Сумма по кредиту")
	c.doc = find(head, "№ документа")
	c.vo = find(head, "ВО")
	c.bank = find(head, "Банк (БИК и наименование)")
	c.purpose = find(head, "Назначение платежа")
	if headerRow < len(rows) {
		sub := rows[headerRow]
		c.debitBlock = find(sub, "Дебет")
		c.creditBlock = find(sub, "Кредит")
	}
	for name, v := range map[string]int{
		"Дата проводки": c.date, "Сумма по дебету": c.debitSum, "Сумма по кредиту": c.creditSum, "№ документа": c.doc,
		"Банк (БИК и наименование)": c.bank, "Назначение платежа": c.purpose, "Дебет": c.debitBlock, "Кредит": c.creditBlock,
	} {
		if v == 0 {
			return c, fmt.Errorf("column %q not found in the statement header", name)
		}
	}
	return c, nil
}

func findAccount(rows [][]string, headerRow int, sheet string) string {
	for r := 0; r < headerRow-1 && r < len(rows); r++ {
		for _, v := range rows[r] {
			if m := accountRe.FindStringSubmatch(v); m != nil {
				return m[1]
			}
		}
	}
	if m := accountRe.FindStringSubmatch(sheet); m != nil {
		return m[1]
	}
	return ""
}

// findPeriod читает «за период с 04 января 2026 г. … по 26 января 2026 г.» из строки шапки.
func findPeriod(rows [][]string, headerRow int) (from, to *model.Date) {
	for r := 0; r < headerRow-1 && r < len(rows); r++ {
		text := strings.Join(rows[r], " ")
		if !strings.Contains(text, "за период") {
			continue
		}
		var found []*model.Date
		for _, m := range dateRu.FindAllStringSubmatch(text, -1) {
			month, ok := months[strings.ToLower(m[2])]
			d, _ := strconv.Atoi(m[1])
			y, _ := strconv.Atoi(m[3])
			if ok {
				found = append(found, &model.Date{Time: time.Date(y, month, d, 0, 0, 0, 0, time.UTC)})
			}
		}
		if len(found) >= 2 {
			return found[0], found[1]
		}
	}
	return nil, nil
}

type block struct{ account, inn, name string }

// parseBlock разбирает «счёт\nИНН\nназвание»; ИНН может отсутствовать, у физлиц в названии бывает хвост «//адрес//…».
func parseBlock(s string) block {
	lines := strings.Split(strings.ReplaceAll(s, "\r", ""), "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	var b block
	if len(lines) > 0 {
		b.account = lines[0]
		lines = lines[1:]
	}
	if len(lines) > 0 && isINN(lines[0]) {
		b.inn = lines[0]
		lines = lines[1:]
	}
	name := strings.Join(lines, " ")
	if i := strings.Index(name, "//"); i >= 0 {
		name = name[:i]
	}
	b.name = label(name)
	return b
}

func isINN(s string) bool {
	if len(s) != 10 && len(s) != 12 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseOperation(ours string, c columns, raw func(col, row int) string, r int, at time.Time) (model.StatementOperation, error) {
	op := model.StatementOperation{Row: r, At: at}

	debit, err := kopecks(raw(c.debitSum, r))
	if err != nil {
		return op, fmt.Errorf("debit amount: %w", err)
	}
	credit, err := kopecks(raw(c.creditSum, r))
	if err != nil {
		return op, fmt.Errorf("credit amount: %w", err)
	}
	switch {
	case debit > 0 && credit > 0:
		return op, fmt.Errorf("both debit and credit amounts are set")
	case debit == 0 && credit == 0:
		return op, fmt.Errorf("no amount")
	}
	debitBlock, creditBlock := parseBlock(raw(c.debitBlock, r)), parseBlock(raw(c.creditBlock, r))
	counter, own := debitBlock, creditBlock
	op.Amount = credit
	if debit > 0 {
		op.Outgoing, op.Amount = true, debit
		counter, own = creditBlock, debitBlock
	}
	if own.account != ours {
		return op, fmt.Errorf("account %q in the row is not the statement account", own.account)
	}
	op.CounterAccount, op.CounterINN, op.CounterName = counter.account, counter.inn, counter.name
	op.DocNumber = raw(c.doc, r)
	if c.vo > 0 {
		op.OperationType = raw(c.vo, r)
	}
	if m := bikRe.FindStringSubmatch(raw(c.bank, r)); m != nil {
		op.BIK, op.BankName = m[1], label(m[2])
	} else {
		op.BankName = label(raw(c.bank, r))
	}
	op.Purpose = label(raw(c.purpose, r))

	var parts []string
	for col := 1; col <= c.purpose; col++ {
		if v := raw(col, r); v != "" {
			parts = append(parts, strings.ReplaceAll(v, "\n", " "))
		}
	}
	op.Raw = strings.Join(parts, " | ")
	return op, nil
}

// kopecks разбирает сумму «4590», «3070.49», «0,00» в копейки; пусто — 0.
func kopecks(s string) (int64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("%q is not an amount", s)
	}
	return int64(v*100 + 0.5), nil
}

// dedupKey опознаёт операцию в пересекающихся выписках: направление, дата (без времени), номер документа,
// сумма, счёт контрагента и назначение. Одинаковые строки внутри файла различаются порядковым номером.
func dedupKey(op model.StatementOperation, seen map[string]int) string {
	dir := "in"
	if op.Outgoing {
		dir = "out"
	}
	base := strings.Join([]string{dir, op.At.Format("2006-01-02"), op.DocNumber, strconv.FormatInt(op.Amount, 10), op.CounterAccount, op.Purpose}, "|")
	seen[base]++
	sum := sha256.Sum256([]byte(base + "|" + strconv.Itoa(seen[base])))
	return hex.EncodeToString(sum[:])
}

// checkFooter сверяет число операций и обороты с итогом внизу выписки и читает остатки.
func checkFooter(st *model.ParsedStatement, rows [][]string, raw func(col, row int) string, headerRow, unreadable int) error {
	rowOf := map[string]int{}
	debitCol, creditCol := 0, 0
	for i := headerRow; i < len(rows); i++ {
		for j, v := range rows[i] {
			switch l := label(v); l {
			case "Количество операций", "Входящий остаток", "Итого оборотов", "Исходящий остаток":
				rowOf[l] = i + 1
			case "Дебет":
				if i+1 > headerRow+1 && debitCol == 0 {
					debitCol = j + 1
				}
			case "Кредит":
				if i+1 > headerRow+1 && creditCol == 0 {
					creditCol = j + 1
				}
			}
		}
	}
	if rowOf["Количество операций"] == 0 || rowOf["Итого оборотов"] == 0 || debitCol == 0 || creditCol == 0 {
		return fmt.Errorf("summary block (Количество операций, Итого оборотов) is missing: the statement is incomplete")
	}
	value := func(label string, col int) (int64, error) { return kopecks(raw(col, rowOf[label])) }

	dc, err1 := strconv.Atoi(raw(debitCol, rowOf["Количество операций"]))
	cc, err2 := strconv.Atoi(raw(creditCol, rowOf["Количество операций"]))
	if err1 != nil || err2 != nil || dc != st.DebitCount || cc != st.CreditCount {
		msg := fmt.Sprintf("summary says %s debit and %s credit operations, the statement has %d and %d",
			raw(debitCol, rowOf["Количество операций"]), raw(creditCol, rowOf["Количество операций"]), st.DebitCount, st.CreditCount)
		if unreadable > 0 {
			msg += fmt.Sprintf(" (rows with an unreadable date: %d)", unreadable)
		}
		return fmt.Errorf("%s", msg)
	}
	dt, err1 := value("Итого оборотов", debitCol)
	ct, err2 := value("Итого оборотов", creditCol)
	if err1 != nil || err2 != nil || dt != st.DebitTotal || ct != st.CreditTotal {
		return fmt.Errorf("summary turnover does not match the sum of operations")
	}
	// Остатки со знаком: кредитовый положительный.
	signed := func(label string) *int64 {
		if rowOf[label] == 0 {
			return nil
		}
		d, e1 := value(label, debitCol)
		c, e2 := value(label, creditCol)
		if e1 != nil || e2 != nil {
			return nil
		}
		v := c - d
		return &v
	}
	st.OpeningBalance, st.ClosingBalance = signed("Входящий остаток"), signed("Исходящий остаток")
	return nil
}

// cellDate читает дату проводки: число Excel (дата-время) или текст «05.01.2026», «05.01.2026 04:31:38», «2026-01-05 04:31:38».
func cellDate(v string) (time.Time, bool) {
	if serial, err := strconv.ParseFloat(v, 64); err == nil {
		if serial < 20000 {
			return time.Time{}, false
		}
		t, err := excelize.ExcelDateToTime(serial, false)
		return t, err == nil
	}
	for _, layout := range []string{"02.01.2006 15:04:05", "02.01.2006 15:04", "02.01.2006", "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02", "02.01.06 15:04:05", "02.01.06"} {
		if t, err := time.Parse(layout, strings.TrimSpace(v)); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
