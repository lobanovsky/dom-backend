// Package sberregistry разбирает реестры платежей Сбера (текстовый файл, cp1251, разделитель «;»).
//
// Строка платежа (13 полей): дата дд-мм-гггг; время чч-мм-сс; поля 3 и 4 (смысл неизвестен);
// номер операции; номер лицевого счёта; ФИО; адрес; поле 9 (смысл неизвестен); сумма платежа;
// сумма к перечислению; комиссия; поле 13 (смысл неизвестен). Исходная строка сохраняется целиком.
// Последняя строка: =N;сумма;сумма к перечислению;комиссия;номер реестра;дата.
package sberregistry

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"dom-backend/internal/model"
)

const fieldCount = 13

var fileNameRe = regexp.MustCompile(`(?:^|_)(\d{10,12})_(\d{20})_\d+\.[Tt][Xx][Tt]$`)

// FileAccount возвращает расчётный счёт получателя из имени файла вида [префикс_]ИНН_СЧЁТ_N.txt.
func FileAccount(fileName string) string {
	if m := fileNameRe.FindStringSubmatch(fileName); m != nil {
		return m[2]
	}
	return ""
}

var accountRe = regexp.MustCompile(`(?:^|\D)(\d{20})(?:\D|$)`)

// AccountsInName возвращает все 20-значные числа из имени файла (кандидаты в номер расчётного счёта).
func AccountsInName(fileName string) []string {
	var out []string
	rest := fileName
	for {
		m := accountRe.FindStringSubmatchIndex(rest)
		if m == nil {
			return out
		}
		out = append(out, rest[m[2]:m[3]])
		rest = rest[m[3]:]
	}
}

// Parse разбирает файл. Ошибки данных собираются по строкам (номер строки файла);
// err возвращается, если файл в целом непригоден (нет итоговой строки, пустой).
func Parse(fileName string, data []byte) (*model.ParsedRegistry, []model.ImportRowError, error) {
	text, err := decode(data)
	if err != nil {
		return nil, nil, err
	}
	reg := &model.ParsedRegistry{FileAccount: FileAccount(fileName)}
	var (
		errs      []model.ImportRowError
		footer    []string
		footerRow int
	)
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		n := i + 1
		if strings.HasPrefix(line, "=") {
			if footer != nil {
				return nil, nil, fmt.Errorf("file has more than one summary line")
			}
			footer, footerRow = strings.Split(line[1:], ";"), n
			continue
		}
		if footer != nil {
			return nil, nil, fmt.Errorf("line %d: data after the summary line", n)
		}
		p, err := parseLine(n, line)
		if err != nil {
			errs = append(errs, model.ImportRowError{Row: n, Error: err.Error()})
			continue
		}
		reg.Payments = append(reg.Payments, p)
		reg.TotalAmount += p.Amount
		reg.TotalTransferred += p.Transferred
		reg.TotalCommission += p.Commission
	}
	if footer == nil {
		return nil, nil, fmt.Errorf("summary line (=N;...) is missing: the file is incomplete")
	}
	if len(errs) > 0 {
		return reg, errs, nil
	}
	if len(reg.Payments) == 0 {
		return nil, nil, fmt.Errorf("file has no payments")
	}
	if err := checkFooter(reg, footer, footerRow); err != nil {
		return nil, nil, err
	}
	return reg, nil, nil
}

func decode(data []byte) (string, error) {
	if utf8.Valid(data) {
		return string(data), nil
	}
	out, err := charmap.Windows1251.NewDecoder().Bytes(data)
	if err != nil {
		return "", fmt.Errorf("cannot decode file as cp1251: %w", err)
	}
	return string(out), nil
}

func parseLine(n int, line string) (model.RegistryPayment, error) {
	f := strings.Split(line, ";")
	if len(f) != fieldCount {
		return model.RegistryPayment{}, fmt.Errorf("expected %d fields, got %d", fieldCount, len(f))
	}
	p := model.RegistryPayment{Line: n, Raw: line, ExternalID: strings.TrimSpace(f[4]), AccountNum: strings.TrimSpace(f[5]),
		PayerName: strings.TrimSpace(f[6]), Address: strings.TrimSpace(f[7])}
	d, err := time.Parse("02-01-2006", strings.TrimSpace(f[0]))
	if err != nil {
		return p, fmt.Errorf("date %q: expected dd-mm-yyyy", f[0])
	}
	p.Date = model.Date{Time: d}
	t, err := time.Parse("15-04-05", strings.TrimSpace(f[1]))
	if err != nil {
		return p, fmt.Errorf("time %q: expected hh-mm-ss", f[1])
	}
	p.Time = model.Clock(t.Format("15:04:05"))
	switch {
	case p.ExternalID == "":
		return p, fmt.Errorf("operation number is empty")
	case p.AccountNum == "":
		return p, fmt.Errorf("account number is empty")
	case p.PayerName == "":
		return p, fmt.Errorf("payer name is empty")
	}
	for i, dst := range []*int64{&p.Amount, &p.Transferred, &p.Commission} {
		if *dst, err = kopecks(f[9+i]); err != nil {
			return p, err
		}
	}
	if p.Amount <= 0 {
		return p, fmt.Errorf("amount must be positive")
	}
	return p, nil
}

func checkFooter(reg *model.ParsedRegistry, f []string, row int) error {
	if len(f) < 6 {
		return fmt.Errorf("line %d: summary line must have 6 fields, got %d", row, len(f))
	}
	count, err := strconv.Atoi(strings.TrimSpace(f[0]))
	if err != nil || count != len(reg.Payments) {
		return fmt.Errorf("summary line says %q payments, the file has %d", f[0], len(reg.Payments))
	}
	for i, want := range []int64{reg.TotalAmount, reg.TotalTransferred, reg.TotalCommission} {
		got, err := kopecks(f[1+i])
		if err != nil || got != want {
			return fmt.Errorf("summary line total %q does not match the sum of payments %s", f[1+i], format(want))
		}
	}
	reg.RegistryNumber = strings.TrimSpace(f[4])
	if d, err := time.Parse("02-01-2006", strings.TrimSpace(f[5])); err == nil {
		reg.RegistryDate = &model.Date{Time: d}
	}
	return nil
}

// kopecks разбирает сумму «8575,32» (или «8575.32», «8575») в копейки.
func kopecks(s string) (int64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 || whole == "" && frac == "" {
		return 0, fmt.Errorf("amount %q: expected a number with at most 2 decimals", s)
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, err1 := strconv.ParseInt(orZero(whole), 10, 64)
	c, err2 := strconv.ParseInt(frac, 10, 64)
	if err1 != nil || err2 != nil || w < 0 {
		return 0, fmt.Errorf("amount %q: expected a number with at most 2 decimals", s)
	}
	return w*100 + c, nil
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

func format(k int64) string { return fmt.Sprintf("%d,%02d", k/100, k%100) }
