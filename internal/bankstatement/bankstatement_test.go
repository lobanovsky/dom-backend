package bankstatement

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

const ours = "40703810338000009999"

type row struct {
	at       time.Time
	debit    string // блок «счёт\nИНН\nназвание»
	credit   string
	debitSum any
	credSum  any
	doc      string
	vo       string
	bank     string
	purpose  string
}

// workbook собирает синтетическую выписку с той же раскладкой, что у СберБизнес (данные вымышленные).
func workbook(t *testing.T, rows []row, footer func(f *excelize.File, sheet string, nextRow int)) []byte {
	t.Helper()
	f := excelize.NewFile()
	sheet := ours
	f.SetSheetName("Sheet1", sheet)
	set := func(cell string, v any) {
		if err := f.SetCellValue(sheet, cell, v); err != nil {
			t.Fatal(err)
		}
	}
	set("B5", "ВЫПИСКА ОПЕРАЦИЙ ПО ЛИЦЕВОМУ СЧЕТУ")
	set("M5", ours)
	set("C7", "за период с 04 января 2026 г.")
	set("O7", " по ")
	set("P7", "26 января 2026 г.")
	for cell, v := range map[string]string{"B10": "Дата проводки", "E10": "Счет", "J10": "Сумма по дебету", "N10": "Сумма по кредиту",
		"O10": "№ документа", "Q10": "ВО", "R10": "Банк (БИК и наименование)", "U10": "Назначение платежа", "E11": "Дебет", "I11": "Кредит"} {
		set(cell, v)
	}
	r := 12
	for _, x := range rows {
		set("B"+itoa(r), x.at)
		set("E"+itoa(r), x.debit)
		set("I"+itoa(r), x.credit)
		if x.debitSum != nil {
			set("J"+itoa(r), x.debitSum)
		}
		if x.credSum != nil {
			set("N"+itoa(r), x.credSum)
		}
		set("O"+itoa(r), x.doc)
		set("Q"+itoa(r), x.vo)
		set("R"+itoa(r), x.bank)
		set("U"+itoa(r), x.purpose)
		r++
	}
	if footer != nil {
		footer(f, sheet, r+2)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func itoa(n int) string {
	return strings.TrimSpace(strings.Join([]string{string(rune('0' + n/100)), string(rune('0' + n/10%10)), string(rune('0' + n%10))}, ""))
}

func standardFooter(dc, cc, debit, credit string) func(*excelize.File, string, int) {
	return func(f *excelize.File, sheet string, r int) {
		set := func(cell string, v any) { _ = f.SetCellValue(sheet, cell, v) }
		n := func(i int) string { return itoa(r + i) }
		set("B"+n(0), "б/с")
		set("H"+n(0), "Дебет")
		set("L"+n(0), "Кредит")
		set("B"+n(2), "Количество операций")
		set("H"+n(2), dc)
		set("L"+n(2), cc)
		set("B"+n(3), "Входящий остаток")
		set("H"+n(3), "0,00")
		set("L"+n(3), "1000.00")
		set("B"+n(4), "Итого оборотов")
		set("H"+n(4), debit)
		set("L"+n(4), credit)
		set("B"+n(5), "Исходящий остаток")
		set("H"+n(5), "0,00")
		set("L"+n(5), "1004.50")
	}
}

var (
	d1 = time.Date(2026, 1, 5, 4, 31, 38, 0, time.UTC)
	d2 = time.Date(2026, 1, 6, 11, 2, 3, 0, time.UTC)
)

func sampleRows() []row {
	return []row{
		{at: d1, debit: ours + "\n9715357654\nТСН \"ТЕСТ\"", credit: "70601810038002720211\n7707083893\nПАО Сбербанк", debitSum: 10.0, doc: "1", vo: "17",
			bank: "БИК 044525225 Московский банк", purpose: "Комиссия за обслуживание"},
		{at: d2, debit: "40817810100044521912\n504908996115\nИВАНОВ ИВАН ИВАНОВИЧ//127422 МОСКВА//4081781//", credit: ours + "\n9715357654\nТСН \"ТЕСТ\"", credSum: 14.5, doc: "811015", vo: "01",
			bank: "БИК 044525974 АО \"ТБанк\"", purpose: "ЛС 0000001101;\nКварплата   по ЛС"},
		{at: d2, debit: "40817810100044521912\n504908996115\nИВАНОВ ИВАН ИВАНОВИЧ", credit: ours + "\n9715357654\nТСН \"ТЕСТ\"", credSum: 14.5, doc: "811015", vo: "01",
			bank: "БИК 044525974 АО \"ТБанк\"", purpose: "ЛС 0000001101; Кварплата по ЛС"},
	}
}

func TestParse(t *testing.T) {
	st, errs, err := Parse(workbook(t, sampleRows(), standardFooter("1", "2", "10,00", "29,00")))
	if err != nil || len(errs) != 0 {
		t.Fatalf("err = %v, errs = %v", err, errs)
	}
	if st.Account != ours || st.PeriodFrom.Format("2006-01-02") != "2026-01-04" || st.PeriodTo.Format("2006-01-02") != "2026-01-26" {
		t.Errorf("header: %+v", st)
	}
	if st.DebitCount != 1 || st.CreditCount != 2 || st.DebitTotal != 1000 || st.CreditTotal != 2900 {
		t.Errorf("totals: %+v", st)
	}
	if *st.OpeningBalance != 100000 || *st.ClosingBalance != 100450 {
		t.Errorf("balances: %d %d", *st.OpeningBalance, *st.ClosingBalance)
	}
	out, in := st.Operations[0], st.Operations[1]
	if !out.Outgoing || out.Amount != 1000 || out.CounterAccount != "70601810038002720211" || out.CounterINN != "7707083893" || out.BIK != "044525225" || out.BankName != "Московский банк" || out.OperationType != "17" {
		t.Errorf("outgoing op: %+v", out)
	}
	if in.Outgoing || in.Amount != 1450 || in.CounterName != "ИВАНОВ ИВАН ИВАНОВИЧ" || in.CounterINN != "504908996115" || in.DocNumber != "811015" ||
		in.Purpose != "ЛС 0000001101; Кварплата по ЛС" || in.At.Format("2006-01-02 15:04:05") != "2026-01-06 11:02:03" {
		t.Errorf("incoming op: %+v", in)
	}
}

func TestDedupKeys(t *testing.T) {
	st, _, err := Parse(workbook(t, sampleRows(), standardFooter("1", "2", "10,00", "29,00")))
	if err != nil {
		t.Fatal(err)
	}
	// две одинаковые операции в файле получают разные ключи (порядковый номер), а те же операции из другой выписки — те же ключи
	if st.Operations[1].DedupKey == st.Operations[2].DedupKey {
		t.Error("identical rows inside one statement must get different keys")
	}
	again, _, err := Parse(workbook(t, sampleRows()[1:], standardFooter("0", "2", "0,00", "29,00")))
	if err != nil {
		t.Fatal(err)
	}
	if again.Operations[0].DedupKey != st.Operations[1].DedupKey || again.Operations[1].DedupKey != st.Operations[2].DedupKey {
		t.Error("the same operations in an overlapping statement must get the same keys")
	}
	// ключ не зависит от времени суток
	rows := sampleRows()
	rows[1].at = rows[1].at.Add(3 * time.Hour)
	shifted, _, err := Parse(workbook(t, rows, standardFooter("1", "2", "10,00", "29,00")))
	if err != nil || shifted.Operations[1].DedupKey != st.Operations[1].DedupKey {
		t.Errorf("key must ignore time of day: %v", err)
	}
}

func TestParseRejects(t *testing.T) {
	good := sampleRows()
	for name, c := range map[string]struct {
		rows   []row
		footer func(*excelize.File, string, int)
		want   string
	}{
		"wrong count":    {good, standardFooter("2", "2", "10,00", "29,00"), "operations"},
		"wrong turnover": {good, standardFooter("1", "2", "10,00", "30,00"), "turnover"},
		"no footer":      {good, nil, "incomplete"},
	} {
		if _, errs, err := Parse(workbook(t, c.rows, c.footer)); err == nil || len(errs) > 0 || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, errs = %v, want containing %q", name, err, errs, c.want)
		}
	}
	if _, _, err := Parse([]byte("not xlsx")); err == nil {
		t.Error("garbage must be rejected")
	}
}

func TestParseRowErrors(t *testing.T) {
	rows := sampleRows()
	rows[0].debit = "40702810000000000000\n1\nЧУЖОЙ СЧЁТ" // не наш счёт на стороне дебета
	rows[1].credSum, rows[1].debitSum = 14.5, 3.0         // обе суммы
	_, errs, err := Parse(workbook(t, rows, standardFooter("1", "2", "10,00", "29,00")))
	if err != nil || len(errs) != 2 || errs[0].Row != 12 || errs[1].Row != 13 {
		t.Fatalf("errs = %v, err = %v", errs, err)
	}
}

func TestKopecks(t *testing.T) {
	for in, want := range map[string]int64{"4590": 459000, "3070.49": 307049, "0,00": 0, "": 0, "10": 1000, "67437.97": 6743797} {
		if got, err := kopecks(in); err != nil || got != want {
			t.Errorf("kopecks(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := kopecks("abc"); err == nil {
		t.Error("expected error")
	}
}
