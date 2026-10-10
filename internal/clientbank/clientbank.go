// Package clientbank разбирает файлы обмена «Клиент-банк — 1С» (1CClientBankExchange, формат 1.0x, чаще 1.03).
//
// Файл — текст «Ключ=Значение»: заголовок (версия, кодировка, период, список расчётных счетов), по секции
// СекцияРасчСчет на счёт и день (остатки и обороты) и секции СекцияДокумент (платёжные поручения, банковские ордера,
// платёжные требования). Направление платежа определяется по тому, какой из счетов наш: если наш счёт плательщика —
// списание, если получателя — поступление. Итоги секций счетов (по дням) сверяются с суммой документов.
package clientbank

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"dom-backend/internal/model"
)

const headerLine = "1CClientBankExchange"

// LooksLike проверяет по первой строке, что это файл обмена 1С (после возможного BOM).
func LooksLike(data []byte) bool {
	head := data
	if len(head) > 64 {
		head = head[:64]
	}
	return strings.HasPrefix(strings.TrimPrefix(string(head), "\xef\xbb\xbf"), headerLine)
}

// Parse разбирает файл: по выписке на каждый наш расчётный счёт, по которому есть документы. Ошибки в документах
// собираются по строкам файла; err возвращается, если файл в целом непригоден или итоги не сходятся.
func Parse(data []byte) ([]*model.ParsedStatement, []model.ImportRowError, error) {
	text, err := decode(data)
	if err != nil {
		return nil, nil, err
	}
	f, err := read(text)
	if err != nil {
		return nil, nil, err
	}
	return f.statements()
}

// decode переводит файл в UTF-8 по заявленной в заголовке кодировке: UTF-8, Windows (cp1251) или DOS (cp866).
func decode(data []byte) (string, error) {
	data = []byte(strings.TrimPrefix(string(data), "\xef\xbb\xbf"))
	if !LooksLike(data) {
		return "", fmt.Errorf("not a 1C client-bank exchange file (%s header is missing)", headerLine)
	}
	if utf8.Valid(data) {
		return string(data), nil
	}
	// Не UTF-8: ключи кириллические, поэтому заявленную кодировку ищем, пробуя обе однобайтные.
	for _, m := range []struct {
		name string
		cm   *charmap.Charmap
		is   func(declared string) bool
	}{
		{"Windows", charmap.Windows1251, func(d string) bool { return strings.HasPrefix(d, "WIN") }},
		{"DOS", charmap.CodePage866, func(d string) bool { return strings.HasPrefix(d, "DOS") || d == "CP866" || d == "866" }},
	} {
		text, err := m.cm.NewDecoder().String(string(data))
		if err != nil {
			continue
		}
		for _, line := range strings.SplitN(text, "\n", 8) {
			if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k == "Кодировка" && m.is(strings.ToUpper(strings.TrimSpace(v))) {
				return text, nil
			}
		}
	}
	return "", fmt.Errorf("unsupported or undeclared encoding (supported: UTF-8, Windows, DOS)")
}

type account struct {
	date                 time.Time
	number               string
	opening, closing     int64
	debited, credited    int64
	line                 int
	hasOpening, hasClose bool
}

type document struct {
	line   int
	kind   string
	fields map[string]string
	raw    []string
}

type file struct {
	version  string
	from, to *model.Date
	accounts []string // РасчСчет из заголовка
	sections []account
	docs     []document
}

// read разбирает строки файла в структуру (без проверок смысла).
func read(text string) (*file, error) {
	f := &file{}
	var (
		section *account
		doc     *document
		fields  map[string]string
	)
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		n := i + 1
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, hasValue := strings.Cut(line, "=")
		switch {
		case n == 1 || line == headerLine:
			continue
		case line == "КонецФайла":
			return f, f.validateHeader()
		case key == "СекцияДокумент":
			doc = &document{line: n, kind: value, fields: map[string]string{}}
			fields = doc.fields
		case line == "КонецДокумента":
			if doc != nil {
				f.docs = append(f.docs, *doc)
			}
			doc, fields = nil, nil
		case line == "СекцияРасчСчет":
			section = &account{line: n}
			fields = map[string]string{}
		case line == "КонецРасчСчет":
			if section != nil {
				a, err := accountFrom(*section, fields)
				if err != nil {
					return nil, fmt.Errorf("line %d: %w", n, err)
				}
				f.sections = append(f.sections, a)
			}
			section, fields = nil, nil
		case !hasValue:
			continue // строка без «=» (продолжение значения или служебная) игнорируется
		case fields != nil:
			fields[key] = strings.TrimSpace(value)
			if doc != nil {
				doc.raw = append(doc.raw, key+"="+strings.TrimSpace(value))
			}
		default: // заголовок файла
			switch key {
			case "ВерсияФормата":
				f.version = strings.TrimSpace(value)
			case "РасчСчет":
				f.accounts = append(f.accounts, strings.TrimSpace(value))
			case "ДатаНачала":
				f.from = parseDatePtr(value)
			case "ДатаКонца":
				f.to = parseDatePtr(value)
			}
		}
	}
	return f, f.validateHeader()
}

func (f *file) validateHeader() error {
	if !strings.HasPrefix(f.version, "1.") {
		return fmt.Errorf("unsupported 1C format version %q (supported: 1.xx, usually 1.03)", f.version)
	}
	return nil
}

func accountFrom(a account, f map[string]string) (account, error) {
	a.number = f["РасчСчет"]
	d, err := parseDate(f["ДатаНачала"])
	if err != nil || a.number == "" {
		return a, fmt.Errorf("account section without a date or a number")
	}
	a.date = d
	var e1, e2, e3, e4 error
	a.opening, e1 = kopecks(f["НачальныйОстаток"])
	a.debited, e2 = kopecks(f["ВсегоСписано"])
	a.credited, e3 = kopecks(f["ВсегоПоступило"])
	a.closing, e4 = kopecks(f["КонечныйОстаток"])
	for _, e := range []error{e1, e2, e3, e4} {
		if e != nil {
			return a, fmt.Errorf("account section %s %s: %w", a.number, f["ДатаНачала"], e)
		}
	}
	return a, nil
}

// statements строит выписки по счетам и сверяет итоги по дням.
func (f *file) statements() ([]*model.ParsedStatement, []model.ImportRowError, error) {
	own := map[string]bool{}
	for _, a := range f.accounts {
		own[a] = true
	}
	for _, s := range f.sections {
		own[s.number] = true
	}
	if len(own) == 0 {
		return nil, nil, fmt.Errorf("the file lists no settlement accounts (РасчСчет)")
	}
	if len(f.docs) == 0 {
		return nil, nil, fmt.Errorf("the file has no payment documents")
	}

	byAccount := map[string]*model.ParsedStatement{}
	var order []string
	seen := map[string]map[string]int{} // счёт -> ключ -> сколько раз встречался
	var errs []model.ImportRowError
	type day struct {
		account string
		date    string
	}
	docDebit, docCredit := map[day]int64{}, map[day]int64{}

	for _, d := range f.docs {
		legs, err := d.operations(own)
		if err != nil {
			errs = append(errs, model.ImportRowError{Row: d.line, Error: err.Error()})
			continue
		}
		for _, leg := range legs {
			st := byAccount[leg.account]
			if st == nil {
				st = &model.ParsedStatement{Account: leg.account, Part: leg.account}
				byAccount[leg.account] = st
				seen[leg.account] = map[string]int{}
				order = append(order, leg.account)
			}
			op := leg.op
			op.DedupKey = DedupKey(op, seen[leg.account])
			st.Operations = append(st.Operations, op)
			k := day{leg.account, op.At.Format("02.01.2006")}
			if op.Outgoing {
				st.DebitCount++
				st.DebitTotal += op.Amount
				docDebit[k] += op.Amount
			} else {
				st.CreditCount++
				st.CreditTotal += op.Amount
				docCredit[k] += op.Amount
			}
		}
	}
	if len(errs) > 0 {
		return nil, errs, nil
	}

	// Итоги секций счетов по дням должны совпасть с суммой документов дня.
	if len(f.sections) > 0 {
		var mismatches []string
		for _, s := range f.sections {
			k := day{s.number, s.date.Format("02.01.2006")}
			if s.debited != docDebit[k] || s.credited != docCredit[k] {
				mismatches = append(mismatches, fmt.Sprintf("%s %s: debit %s in the section, %s in documents; credit %s in the section, %s in documents",
					s.number, k.date, money(s.debited), money(docDebit[k]), money(s.credited), money(docCredit[k])))
			}
		}
		if len(mismatches) > 0 {
			shown := mismatches
			if len(shown) > 3 {
				shown = shown[:3]
			}
			return nil, nil, fmt.Errorf("day totals do not match the documents (%d days): %s", len(mismatches), strings.Join(shown, " | "))
		}
	}

	sort.Strings(order)
	var out []*model.ParsedStatement
	for _, number := range order {
		st := byAccount[number]
		f.fillPeriodAndBalances(st)
		out = append(out, st)
	}
	for _, st := range out {
		st.MultiPart = len(out) > 1
	}
	return out, nil, nil
}

// fillPeriodAndBalances: период из заголовка (или из дат операций), остатки из первой и последней секции счёта.
func (f *file) fillPeriodAndBalances(st *model.ParsedStatement) {
	st.PeriodFrom, st.PeriodTo = f.from, f.to
	var first, last *account
	for i := range f.sections {
		s := &f.sections[i]
		if s.number != st.Account {
			continue
		}
		if first == nil || s.date.Before(first.date) {
			first = s
		}
		if last == nil || s.date.After(last.date) {
			last = s
		}
	}
	if first != nil {
		st.OpeningBalance = &first.opening
		st.ClosingBalance = &last.closing
	}
	if st.PeriodFrom == nil || st.PeriodTo == nil {
		lo, hi := st.Operations[0].At, st.Operations[0].At
		for _, op := range st.Operations {
			if op.At.Before(lo) {
				lo = op.At
			}
			if op.At.After(hi) {
				hi = op.At
			}
		}
		st.PeriodFrom, st.PeriodTo = &model.Date{Time: lo}, &model.Date{Time: hi}
	}
}

type leg struct {
	account string
	op      model.StatementOperation
}

// operations превращает документ в операции по нашим счетам: списание, если наш счёт у плательщика, и/или поступление.
func (d document) operations(own map[string]bool) ([]leg, error) {
	amount, err := kopecks(d.fields["Сумма"])
	if err != nil || amount <= 0 {
		return nil, fmt.Errorf("amount %q is not valid", d.fields["Сумма"])
	}
	payer, receiver := payerAccount(d.fields), receiverAccount(d.fields)
	var out []leg
	if own[payer] {
		at, err := firstDate(d.fields["ДатаСписано"], d.fields["Дата"])
		if err != nil {
			return nil, fmt.Errorf("debit date is not valid")
		}
		out = append(out, leg{payer, d.operation(amount, at, true)})
	}
	if own[receiver] {
		at, err := firstDate(d.fields["ДатаПоступило"], d.fields["Дата"])
		if err != nil {
			return nil, fmt.Errorf("credit date is not valid")
		}
		out = append(out, leg{receiver, d.operation(amount, at, false)})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("neither the payer account %q nor the receiver account %q is among the file's accounts", payer, receiver)
	}
	return out, nil
}

func payerAccount(f map[string]string) string {
	if v := f["ПлательщикРасчСчет"]; v != "" {
		return v
	}
	return f["ПлательщикСчет"]
}

func receiverAccount(f map[string]string) string {
	if v := f["ПолучательРасчСчет"]; v != "" {
		return v
	}
	return f["ПолучательСчет"]
}

// operation: контрагент — противоположная сторона (у списания получатель, у поступления плательщик).
func (d document) operation(amount int64, at time.Time, outgoing bool) model.StatementOperation {
	side := "Плательщик"
	if outgoing {
		side = "Получатель"
	}
	f := d.fields
	counterAccount := f[side+"РасчСчет"]
	if counterAccount == "" {
		counterAccount = f[side+"Счет"]
	}
	inn := f[side+"ИНН"]
	if inn == "0" {
		inn = "" // у плательщика-физлица ИНН нет
	}
	name := f[side]
	if i := strings.Index(name, "//"); i >= 0 {
		name = name[:i]
	}
	return model.StatementOperation{
		Row: d.line, At: at, Outgoing: outgoing, Amount: amount,
		CounterAccount: counterAccount, CounterINN: inn, CounterName: Collapse(name),
		DocNumber: f["Номер"], OperationType: f["ВидОплаты"],
		BIK: f[side+"БИК"], BankName: Collapse(f[side+"Банк1"]),
		Purpose: Collapse(f["НазначениеПлатежа"]), Raw: strings.Join(d.raw, "; "),
	}
}

// Collapse схлопывает пробелы и переводы строк: так выписки 1С и Sber API дают одинаковый ключ дедупликации.
func Collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func firstDate(values ...string) (time.Time, error) {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return parseDate(v)
		}
	}
	return time.Time{}, fmt.Errorf("no date")
}

func parseDate(v string) (time.Time, error) {
	return time.Parse("02.01.2006", strings.TrimSpace(v))
}

func parseDatePtr(v string) *model.Date {
	if t, err := parseDate(v); err == nil {
		return &model.Date{Time: t}
	}
	return nil
}

// kopecks разбирает сумму «12277.6», «9714,77», «318638.47» в копейки; пусто — 0.
func kopecks(s string) (int64, error) {
	s = strings.NewReplacer(",", ".", " ", "", " ", "").Replace(strings.TrimSpace(s))
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("%q is not an amount", s)
	}
	return int64(v*100 + 0.5), nil
}

func money(k int64) string { return fmt.Sprintf("%d.%02d", k/100, k%100) }

// DedupKey опознаёт операцию в пересекающихся выгрузках: направление, дата, номер документа, сумма, счёт контрагента
// и назначение; одинаковые документы внутри файла различаются порядковым номером.
func DedupKey(op model.StatementOperation, seen map[string]int) string {
	dir := "in"
	if op.Outgoing {
		dir = "out"
	}
	base := strings.Join([]string{dir, op.At.Format("2006-01-02"), op.DocNumber, strconv.FormatInt(op.Amount, 10), op.CounterAccount, op.Purpose}, "|")
	seen[base]++
	sum := sha256.Sum256([]byte(base + "|" + strconv.Itoa(seen[base])))
	return hex.EncodeToString(sum[:])
}
