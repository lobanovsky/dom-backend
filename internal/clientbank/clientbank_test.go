package clientbank

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

const (
	ours   = "40703810338000009999"
	second = "40705810238000000478"
)

type doc struct {
	number, date, amount    string
	payerAcc, receiverAcc   string
	payerName, receiverName string
	payerINN, receiverINN   string
	purpose                 string
	debitDate, creditDate   string
}

func (d doc) text() string {
	return fmt.Sprintf("СекцияДокумент=Платежное поручение\nНомер=%s\nДата=%s\nСумма=%s\nПлательщикСчет=%s\nДатаСписано=%s\nПлательщик=%s\nПлательщикИНН=%s\nПлательщикРасчСчет=%s\nПлательщикБанк1=Банк плательщика\nПлательщикБИК=044525974\nПолучательСчет=%s\nДатаПоступило=%s\nПолучатель=%s\nПолучательИНН=%s\nПолучательРасчСчет=%s\nПолучательБанк1=Банк получателя, г.Москва\nПолучательБИК=044525225\nВидОплаты=01\nНазначениеПлатежа=%s\nКонецДокумента\n",
		d.number, d.date, d.amount, d.payerAcc, d.debitDate, d.payerName, d.payerINN, d.payerAcc, d.receiverAcc, d.creditDate, d.receiverName, d.receiverINN, d.receiverAcc, d.purpose)
}

func incoming(number, date, amount, purpose string) doc {
	return doc{number: number, date: date, amount: amount, payerAcc: "40817810100044521912", receiverAcc: ours, payerName: "ИВАНОВ ИВАН ИВАНОВИЧ//127422 МОСКВА//", receiverName: "ТСН",
		payerINN: "0", receiverINN: "9715357654", purpose: purpose, creditDate: date}
}

func outgoing(number, date, amount string) doc {
	return doc{number: number, date: date, amount: amount, payerAcc: ours, receiverAcc: "40702810938000017078", payerName: "ТСН", receiverName: `ООО "Лифт"`,
		payerINN: "9715357654", receiverINN: "7722786153", purpose: "Оплата по счёту № 5", debitDate: date}
}

// exchange собирает файл 1С: заголовок, секции счетов по дням и документы.
func exchange(sections string, docs ...doc) string {
	var b strings.Builder
	b.WriteString("1CClientBankExchange\nВерсияФормата=1.03\nКодировка=UTF-8\nОтправитель=Тест\nПолучатель=\nДатаСоздания=08.10.2026\nВремяСоздания=10:00:00\nДатаНачала=05.01.2026\nДатаКонца=06.01.2026\nРасчСчет=" + ours + "\nРасчСчет=" + second + "\n")
	b.WriteString(sections)
	for _, d := range docs {
		b.WriteString(d.text())
	}
	b.WriteString("КонецФайла\n")
	return b.String()
}

func section(acc, date, opening, debited, credited, closing string) string {
	return fmt.Sprintf("СекцияРасчСчет\nДатаНачала=%s\nДатаКонца=%s\nНачальныйОстаток=%s\nРасчСчет=%s\nВсегоСписано=%s\nВсегоПоступило=%s\nКонечныйОстаток=%s\nКонецРасчСчет\n", date, date, opening, acc, debited, credited, closing)
}

var goodSections = section(ours, "05.01.2026", "1000", "0", "100.5", "1100.5") +
	section(ours, "06.01.2026", "1100.5", "20", "0", "1080.5") +
	section(second, "05.01.2026", "50", "0", "0", "50") +
	section(second, "06.01.2026", "50", "0", "0", "50")

func goodFile() string {
	return exchange(goodSections, incoming("1", "05.01.2026", "100.5", "ЛС 0000001101"), outgoing("2", "06.01.2026", "20.0"))
}

func TestParse(t *testing.T) {
	sts, errs, err := Parse([]byte(goodFile()))
	if err != nil || len(errs) != 0 {
		t.Fatalf("err = %v, errs = %v", err, errs)
	}
	if len(sts) != 1 {
		t.Fatalf("only the account with documents gets a statement, got %d", len(sts))
	}
	st := sts[0]
	if st.Account != ours || st.MultiPart || st.PeriodFrom.Format("2006-01-02") != "2026-01-05" || st.PeriodTo.Format("2006-01-02") != "2026-01-06" {
		t.Errorf("header: %+v", st)
	}
	if st.DebitCount != 1 || st.CreditCount != 1 || st.DebitTotal != 2000 || st.CreditTotal != 10050 || *st.OpeningBalance != 100000 || *st.ClosingBalance != 108050 {
		t.Errorf("totals: %+v opening=%d closing=%d", st, *st.OpeningBalance, *st.ClosingBalance)
	}
	in, out := st.Operations[0], st.Operations[1]
	if in.Outgoing || in.Amount != 10050 || in.CounterAccount != "40817810100044521912" || in.CounterName != "ИВАНОВ ИВАН ИВАНОВИЧ" || in.CounterINN != "" ||
		in.DocNumber != "1" || in.OperationType != "01" || in.BIK != "044525974" || in.BankName != "Банк плательщика" || in.Purpose != "ЛС 0000001101" ||
		in.At.Format("2006-01-02") != "2026-01-05" {
		t.Errorf("incoming: %+v", in)
	}
	if !out.Outgoing || out.Amount != 2000 || out.CounterAccount != "40702810938000017078" || out.CounterName != `ООО "Лифт"` || out.CounterINN != "7722786153" ||
		out.BIK != "044525225" || out.BankName != "Банк получателя, г.Москва" || out.At.Format("2006-01-02") != "2026-01-06" {
		t.Errorf("outgoing: %+v", out)
	}
}

func TestSeveralAccounts(t *testing.T) {
	d2 := incoming("3", "05.01.2026", "10", "кв 5")
	d2.receiverAcc = second
	sections := section(ours, "05.01.2026", "1000", "0", "100.5", "1100.5") + section(second, "05.01.2026", "50", "0", "10", "60")
	sts, errs, err := Parse([]byte(exchange(sections, incoming("1", "05.01.2026", "100.5", "x"), d2)))
	if err != nil || len(errs) != 0 || len(sts) != 2 {
		t.Fatalf("err = %v, errs = %v, statements = %d", err, errs, len(sts))
	}
	if sts[0].Account != ours || sts[1].Account != second || !sts[0].MultiPart || !sts[1].MultiPart || sts[1].Part != second {
		t.Errorf("statements: %s %s multi=%v", sts[0].Account, sts[1].Account, sts[0].MultiPart)
	}
}

func TestDayTotalsMustMatchDocuments(t *testing.T) {
	bad := section(ours, "05.01.2026", "1000", "0", "999", "1999") + section(ours, "06.01.2026", "1999", "20", "0", "1979")
	_, _, err := Parse([]byte(exchange(bad, incoming("1", "05.01.2026", "100.5", "x"), outgoing("2", "06.01.2026", "20"))))
	if err == nil || !strings.Contains(err.Error(), "day totals do not match the documents (1 days)") ||
		!strings.Contains(err.Error(), ours+" 05.01.2026: debit 0.00 in the section, 0.00 in documents; credit 999.00 in the section, 100.50 in documents") {
		t.Errorf("err = %v", err)
	}
}

func TestEncodings(t *testing.T) {
	for name, encode := range map[string]struct {
		cm   *charmap.Charmap
		decl string
	}{"cp1251": {charmap.Windows1251, "Windows"}, "cp866": {charmap.CodePage866, "DOS"}} {
		text := strings.Replace(goodFile(), "Кодировка=UTF-8", "Кодировка="+encode.decl, 1)
		raw, err := encode.cm.NewEncoder().String(text)
		if err != nil {
			t.Fatal(err)
		}
		sts, errs, err := Parse([]byte(raw))
		if err != nil || len(errs) != 0 || len(sts) != 1 || sts[0].Operations[0].CounterName != "ИВАНОВ ИВАН ИВАНОВИЧ" {
			t.Errorf("%s: err = %v, errs = %v", name, err, errs)
		}
	}
	// BOM и CRLF
	if sts, _, err := Parse([]byte("\xef\xbb\xbf" + strings.ReplaceAll(goodFile(), "\n", "\r\n"))); err != nil || len(sts) != 1 {
		t.Errorf("BOM + CRLF: %v", err)
	}
	// заявлен UTF-8, а байты нет: понятная ошибка
	raw, _ := charmap.Windows1251.NewEncoder().String(goodFile())
	if _, _, err := Parse([]byte(raw)); err == nil {
		t.Error("cp1251 bytes under a UTF-8 declaration must be rejected")
	}
}

func TestRejects(t *testing.T) {
	for name, c := range map[string]struct{ in, want string }{
		"not 1C":           {"просто текст\n", "not a 1C client-bank exchange file"},
		"old version":      {strings.Replace(goodFile(), "ВерсияФормата=1.03", "ВерсияФормата=2.0", 1), "unsupported 1C format version"},
		"no documents":     {exchange(""), "no payment documents"},
		"unknown encoding": {strings.Replace(goodFile(), "Кодировка=UTF-8", "Кодировка=KOI8", 1) + "\xff", "encoding"},
	} {
		if _, _, err := Parse([]byte(c.in)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want containing %q", name, err, c.want)
		}
	}
}

func TestDocumentErrors(t *testing.T) {
	stranger := incoming("7", "05.01.2026", "5", "x")
	stranger.receiverAcc = "40702810000000000000"
	noAmount := incoming("8", "05.01.2026", "abc", "x")
	_, errs, err := Parse([]byte(exchange("", stranger, noAmount)))
	if err != nil || len(errs) != 2 || !strings.Contains(errs[0].Error, "neither the payer account") || !strings.Contains(errs[1].Error, `amount "abc" is not valid`) || errs[0].Row == errs[1].Row {
		t.Fatalf("errs = %v, err = %v", errs, err)
	}
}

// Перевод между своими счетами даёт две операции: списание на одном счёте и поступление на другом.
func TestTransferBetweenOwnAccounts(t *testing.T) {
	tr := doc{number: "9", date: "05.01.2026", amount: "30", payerAcc: ours, receiverAcc: second, payerName: "ТСН", receiverName: "ТСН", payerINN: "9715357654", receiverINN: "9715357654",
		purpose: "Перевод на спецсчёт", debitDate: "05.01.2026", creditDate: "05.01.2026"}
	sts, errs, err := Parse([]byte(exchange("", tr)))
	if err != nil || len(errs) != 0 || len(sts) != 2 {
		t.Fatalf("err = %v, errs = %v, statements = %d", err, errs, len(sts))
	}
	byAcc := map[string]bool{}
	for _, st := range sts {
		byAcc[st.Account] = st.Operations[0].Outgoing
	}
	if !byAcc[ours] || byAcc[second] {
		t.Errorf("directions: %v", byAcc)
	}
}

func TestDedupKeysAcrossOverlappingFiles(t *testing.T) {
	a, _, err := Parse([]byte(exchange("", incoming("1", "10.01.2026", "300.25", "оплата"), incoming("1", "10.01.2026", "300.25", "оплата"))))
	if err != nil {
		t.Fatal(err)
	}
	if a[0].Operations[0].DedupKey == a[0].Operations[1].DedupKey {
		t.Error("identical documents inside one file must get different keys")
	}
	b, _, err := Parse([]byte(exchange("", incoming("1", "10.01.2026", "300.25", "оплата"), incoming("2", "12.01.2026", "5", "новая"))))
	if err != nil {
		t.Fatal(err)
	}
	if b[0].Operations[0].DedupKey != a[0].Operations[0].DedupKey {
		t.Error("the same document in an overlapping file must get the same key")
	}
}

func TestLooksLikeAndMoney(t *testing.T) {
	if !LooksLike([]byte("\xef\xbb\xbf1CClientBankExchange\n")) || LooksLike([]byte("hello")) {
		t.Error("LooksLike")
	}
	for in, want := range map[string]int64{"12277.6": 1227760, "9714,77": 971477, "0": 0, "": 0, "318638.47": 31863847, "1 000.50": 100050} {
		if got, err := kopecks(in); err != nil || got != want {
			t.Errorf("kopecks(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := kopecks("abc"); err == nil {
		t.Error("expected an error")
	}
}
