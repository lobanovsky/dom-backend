package bankstatement

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"dom-backend/internal/model"
)

const ours = "40703810338000009999"

// parseOne разбирает файл с одним листом и возвращает его выписку.
func parseOne(data []byte) (*model.ParsedStatement, []model.ImportRowError, error) {
	sts, errs, err := Parse(data)
	if len(sts) > 0 {
		return sts[0], errs, err
	}
	return nil, errs, err
}

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
	f.SetSheetName("Sheet1", ours)
	buildSheet(t, f, ours, ours, rows, footer)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// buildSheet заполняет лист выписки по счёту account.
func buildSheet(t *testing.T, f *excelize.File, sheet, account string, rows []row, footer func(f *excelize.File, sheet string, nextRow int)) {
	t.Helper()
	set := func(cell string, v any) {
		if err := f.SetCellValue(sheet, cell, v); err != nil {
			t.Fatal(err)
		}
	}
	set("B5", "ВЫПИСКА ОПЕРАЦИЙ ПО ЛИЦЕВОМУ СЧЕТУ")
	set("M5", account)
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
		set("E"+itoa(r), strings.ReplaceAll(x.debit, ours, account))
		set("I"+itoa(r), strings.ReplaceAll(x.credit, ours, account))
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
	st, errs, err := parseOne(workbook(t, sampleRows(), standardFooter("1", "2", "10,00", "29,00")))
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
	st, _, err := parseOne(workbook(t, sampleRows(), standardFooter("1", "2", "10,00", "29,00")))
	if err != nil {
		t.Fatal(err)
	}
	// две одинаковые операции в файле получают разные ключи (порядковый номер), а те же операции из другой выписки — те же ключи
	if st.Operations[1].DedupKey == st.Operations[2].DedupKey {
		t.Error("identical rows inside one statement must get different keys")
	}
	again, _, err := parseOne(workbook(t, sampleRows()[1:], standardFooter("0", "2", "0,00", "29,00")))
	if err != nil {
		t.Fatal(err)
	}
	if again.Operations[0].DedupKey != st.Operations[1].DedupKey || again.Operations[1].DedupKey != st.Operations[2].DedupKey {
		t.Error("the same operations in an overlapping statement must get the same keys")
	}
	// ключ не зависит от времени суток
	rows := sampleRows()
	rows[1].at = rows[1].at.Add(3 * time.Hour)
	shifted, _, err := parseOne(workbook(t, rows, standardFooter("1", "2", "10,00", "29,00")))
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
		if _, errs, err := parseOne(workbook(t, c.rows, c.footer)); err == nil || len(errs) > 0 || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, errs = %v, want containing %q", name, err, errs, c.want)
		}
	}
	if _, _, err := parseOne([]byte("not xlsx")); err == nil {
		t.Error("garbage must be rejected")
	}
}

func TestParseRowErrors(t *testing.T) {
	rows := sampleRows()
	rows[0].debit = "40702810000000000000\n1\nЧУЖОЙ СЧЁТ" // не наш счёт на стороне дебета
	rows[1].credSum, rows[1].debitSum = 14.5, 3.0         // обе суммы
	_, errs, err := parseOne(workbook(t, rows, standardFooter("1", "2", "10,00", "29,00")))
	if err != nil || len(errs) != 2 || errs[0].Row != 12 || errs[1].Row != 13 {
		t.Fatalf("errs = %v, err = %v", errs, err)
	}
}

func TestKopecks(t *testing.T) {
	for in, want := range map[string]int64{"4590": 459000, "3070.49": 307049, "0,00": 0, "": 0, "10": 1000, "67437.97": 6743797,
		"740 895,36 (П)": 74089536, "1\u00a0000,50": 100050, "0,00 (П)": 0, "10.0": 1000} {
		if got, err := kopecks(in); err != nil || got != want {
			t.Errorf("kopecks(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := kopecks("abc"); err == nil {
		t.Error("expected error")
	}
}

func TestParseTextDates(t *testing.T) {
	rows := sampleRows()
	f := excelize.NewFile()
	_ = f
	// те же операции, но дата проводки записана текстом: выписки старых лет бывают такими
	data := workbook(t, rows, standardFooter("1", "2", "10,00", "29,00"))
	wb, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{"05.01.2026 04:31:38", "06.01.2026", "2026-01-06 11:02:03"} {
		if err := wb.SetCellValue(ours, "B"+itoa(12+i), text); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := wb.Write(&buf); err != nil {
		t.Fatal(err)
	}
	st, errs, err := parseOne(buf.Bytes())
	if err != nil || len(errs) != 0 || len(st.Operations) != 3 {
		t.Fatalf("text dates: err = %v, errs = %v, ops = %d", err, errs, len(st.Operations))
	}
	if st.Operations[0].At.Format("2006-01-02 15:04:05") != "2026-01-05 04:31:38" || st.Operations[1].At.Format("2006-01-02") != "2026-01-06" {
		t.Errorf("dates: %v %v", st.Operations[0].At, st.Operations[1].At)
	}
}

func TestMismatchMentionsUnreadableDates(t *testing.T) {
	data := workbook(t, sampleRows(), standardFooter("1", "2", "10,00", "29,00"))
	wb, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := wb.SetCellValue(ours, "B13", "вчера"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := wb.Write(&buf); err != nil {
		t.Fatal(err)
	}
	_, _, err = parseOne(buf.Bytes())
	if err == nil || !strings.Contains(err.Error(), "summary says 1 debit and 2 credit") || !strings.Contains(err.Error(), "has 1 and 1") || !strings.Contains(err.Error(), "unreadable date: 1") {
		t.Errorf("err = %v", err)
	}
}

// oldFooter — итог выгрузки прошлых лет: числа вместо строк, остатки с пробелами-разделителями и пометкой «(П)».
func oldFooter(dc, cc float64, debit, credit string) func(*excelize.File, string, int) {
	return func(f *excelize.File, sheet string, r int) {
		set := func(cell string, v any) { _ = f.SetCellValue(sheet, cell, v) }
		n := func(i int) string { return itoa(r + i) }
		set("B"+n(0), "б/с")
		set("H"+n(0), "Дебет")
		set("L"+n(0), "Кредит")
		set("T"+n(0), "Всего")
		set("B"+n(2), "Количество операций")
		set("H"+n(2), dc)
		set("L"+n(2), cc)
		set("B"+n(3), "Входящий остаток")
		set("H"+n(3), "0,00")
		set("L"+n(3), "1 000,00 (П)")
		set("T"+n(3), "1 января 2020 г.")
		set("B"+n(4), "Итого оборотов")
		set("H"+n(4), debit)
		set("L"+n(4), credit)
		set("B"+n(5), "Исходящий остаток")
		set("H"+n(5), "0,00")
		set("L"+n(5), "1 004,50 (П)")
	}
}

// oldRows — те же операции в оформлении прошлых лет: у плательщика-физлица ИНН «0», банк через запятую.
func oldRows() []row {
	rows := sampleRows()
	rows[1].debit = "40817810505004023978\n0\nЗВЯГИНЦЕВА ЮЛИЯ ВИКТОРОВНА //"
	for i := range rows {
		rows[i].bank = "БИК 044525974, АО \"Тинькофф Банк\" Г. Москва"
	}
	return rows
}

func TestParseOldFormat(t *testing.T) {
	st, errs, err := parseOne(workbook(t, oldRows(), oldFooter(1, 2, "10.0", "29.0")))
	if err != nil || len(errs) != 0 {
		t.Fatalf("old format: err = %v, errs = %v", err, errs)
	}
	if st.DebitCount != 1 || st.CreditCount != 2 || *st.OpeningBalance != 100000 || *st.ClosingBalance != 100450 {
		t.Errorf("totals: %+v opening=%d closing=%d", st, *st.OpeningBalance, *st.ClosingBalance)
	}
	in := st.Operations[1]
	if in.CounterName != "ЗВЯГИНЦЕВА ЮЛИЯ ВИКТОРОВНА" || in.CounterINN != "" || in.BIK != "044525974" || in.BankName != `АО "Тинькофф Банк" Г. Москва` {
		t.Errorf("counterparty of an old-format row: %+v", in)
	}
}

func TestParseSeveralSheets(t *testing.T) {
	const other = "40703810838000014811"
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", ours)
	buildSheet(t, f, ours, ours, sampleRows(), standardFooter("1", "2", "10,00", "29,00"))
	// второй лист — пустой (по этому счёту операций за период не было), третий — с операциями
	if _, err := f.NewSheet(other); err != nil {
		t.Fatal(err)
	}
	buildSheet(t, f, other, other, nil, oldFooter(0, 0, "0.0", "0.0"))
	const third = "40703810338000004376"
	if _, err := f.NewSheet(third); err != nil {
		t.Fatal(err)
	}
	buildSheet(t, f, third, third, sampleRows()[1:2], oldFooter(0, 1, "0.0", "14.5"))
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	sts, errs, err := Parse(buf.Bytes())
	if err != nil || len(errs) != 0 {
		t.Fatalf("err = %v, errs = %v", err, errs)
	}
	if len(sts) != 2 || sts[0].Account != ours || sts[1].Account != third || !sts[0].MultiSheet || !sts[1].MultiSheet || sts[1].Sheet != third {
		t.Fatalf("statements: %+v", sts)
	}
	// файл, где все листы пусты, — не выписка
	f2 := excelize.NewFile()
	f2.SetSheetName("Sheet1", other)
	buildSheet(t, f2, other, other, nil, oldFooter(0, 0, "0.0", "0.0"))
	var buf2 bytes.Buffer
	_ = f2.Write(&buf2)
	if _, _, err := Parse(buf2.Bytes()); err == nil || !strings.Contains(err.Error(), "no operations") {
		t.Errorf("all-empty workbook: err = %v", err)
	}
}

func TestColumnSynonyms(t *testing.T) {
	data := workbook(t, sampleRows(), standardFooter("1", "2", "10,00", "29,00"))
	wb, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	_ = wb.SetCellValue(ours, "B10", "Дата операции")
	_ = wb.SetCellValue(ours, "U10", "Назначение")
	var buf bytes.Buffer
	_ = wb.Write(&buf)
	if st, errs, err := parseOne(buf.Bytes()); err != nil || len(errs) != 0 || len(st.Operations) != 3 {
		t.Fatalf("synonyms: err = %v, errs = %v", err, errs)
	}
}

// В старых выгрузках строки склеены вертикальным объединением ячеек; пустая строка внутри объединения не должна
// читаться как повтор операции выше (excelize отдаёт значение якорной ячейки для всего диапазона).
func TestMergedCellsAreNotDuplicatedOperations(t *testing.T) {
	data := workbook(t, sampleRows(), standardFooter("1", "2", "10,00", "29,00"))
	wb, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, col := range []string{"B", "E", "I", "J", "N", "O", "Q", "R", "U"} {
		if err := wb.MergeCell(ours, col+"14", col+"15"); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	_ = wb.Write(&buf)
	st, errs, err := parseOne(buf.Bytes())
	if err != nil || len(errs) != 0 || len(st.Operations) != 3 || st.CreditCount != 2 {
		t.Fatalf("merged rows must not add operations: err = %v, errs = %v, ops = %d", err, errs, len(st.Operations))
	}
}

func TestUnknownLayoutErrorListsHeaderLabels(t *testing.T) {
	data := workbook(t, sampleRows(), standardFooter("1", "2", "10,00", "29,00"))
	wb, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	_ = wb.SetCellValue(ours, "N10", "Приход") // колонка с новой подписью
	var buf bytes.Buffer
	_ = wb.Write(&buf)
	_, _, err = Parse(buf.Bytes())
	if err == nil || !strings.Contains(err.Error(), `column "Сумма по кредиту" not found`) || !strings.Contains(err.Error(), `"Приход"`) || !strings.Contains(err.Error(), `"Назначение платежа"`) {
		t.Errorf("err = %v", err)
	}
}

// Банк иногда склеивает счёт и название в одной строке без ИНН: «40703810338000004376ТСН "МР17ДОМ1"».
func TestBlockWithoutNewlines(t *testing.T) {
	rows := sampleRows()
	rows[0].debit = ours + `ТСН "ТЕСТ"`
	rows[0].credit = `70601810038002720211ПАО Сбербанк`
	rows[1].debit = `40817810400075911534Иванов Иван Иванович`
	st, errs, err := parseOne(workbook(t, rows, standardFooter("1", "2", "10,00", "29,00")))
	if err != nil || len(errs) != 0 {
		t.Fatalf("err = %v, errs = %v", err, errs)
	}
	if out := st.Operations[0]; !out.Outgoing || out.CounterAccount != "70601810038002720211" || out.CounterName != "ПАО Сбербанк" || out.CounterINN != "" {
		t.Errorf("outgoing with a glued block: %+v", out)
	}
	if in := st.Operations[1]; in.CounterAccount != "40817810400075911534" || in.CounterName != "Иванов Иван Иванович" {
		t.Errorf("incoming with a glued block: %+v", in)
	}
}

func TestTurnoverMismatchShowsAmounts(t *testing.T) {
	_, _, err := parseOne(workbook(t, sampleRows(), standardFooter("1", "2", "10,00", "67,00")))
	if err == nil || !strings.Contains(err.Error(), "credit 67.00 in the summary, 29.00 in rows") {
		t.Errorf("err = %v", err)
	}
}
