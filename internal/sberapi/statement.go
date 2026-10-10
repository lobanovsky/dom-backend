package sberapi

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"dom-backend/internal/clientbank"
	"dom-backend/internal/model"
)

// Transaction — операция рублёвой выписки (нужные нам поля ответа /v2/statement/transactions).
type Transaction struct {
	Direction string `json:"direction"` // DEBIT — списание с нашего счёта, CREDIT — поступление
	Amount    struct {
		Amount json.Number `json:"amount"`
	} `json:"amount"`
	Number         string `json:"number"`
	OperationCode  string `json:"operationCode"`
	OperationDate  string `json:"operationDate"`
	DocumentDate   string `json:"documentDate"`
	PaymentPurpose string `json:"paymentPurpose"`
	UUID           string `json:"uuid"`
	RurTransfer    struct {
		PayeeAccount  string `json:"payeeAccount"`
		PayeeBankBic  string `json:"payeeBankBic"`
		PayeeBankName string `json:"payeeBankName"`
		PayeeInn      string `json:"payeeInn"`
		PayeeName     string `json:"payeeName"`
		PayerAccount  string `json:"payerAccount"`
		PayerBankBic  string `json:"payerBankBic"`
		PayerBankName string `json:"payerBankName"`
		PayerInn      string `json:"payerInn"`
		PayerName     string `json:"payerName"`
	} `json:"rurTransfer"`
}

// kopecks переводит десятичную строку в копейки без потери точности (1.01 → 101).
func kopecks(n json.Number) (int64, error) {
	r, ok := new(big.Rat).SetString(string(n))
	if !ok {
		return 0, fmt.Errorf("bad amount %q", n)
	}
	r.Mul(r, big.NewRat(100, 1))
	if !r.IsInt() {
		return 0, fmt.Errorf("amount %q has fractions of a kopeck", n)
	}
	return r.Num().Int64(), nil
}

func parseTime(s string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02T15:04:05.999999999Z07:00", "2006-01-02T15:04:05.999999999", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Statement собирает выписку нашего счёта из операций за период. Операции идут в порядке дней, как их вернул банк:
// порядковый номер одинаковых операций (часть ключа дедупликации) считается внутри дня, так же как для файлов 1С.
func Statement(account string, from, to time.Time, txs []Transaction) (*model.ParsedStatement, error) {
	pf, pt := model.Date{Time: from}, model.Date{Time: to}
	st := &model.ParsedStatement{Account: account, PeriodFrom: &pf, PeriodTo: &pt}
	seen := map[string]int{}
	for i, t := range txs {
		amount, err := kopecks(t.Amount.Amount)
		if err != nil {
			return nil, fmt.Errorf("operation %d: %w", i+1, err)
		}
		at, ok := parseTime(t.OperationDate)
		if !ok {
			if at, ok = parseTime(t.DocumentDate); !ok {
				return nil, fmt.Errorf("operation %d: no operation date", i+1)
			}
		}
		rt := t.RurTransfer
		op := model.StatementOperation{
			Row: i + 1, At: at, Amount: amount, DocNumber: t.Number, OperationType: t.OperationCode,
			Purpose: oneLine(t.PaymentPurpose),
		}
		// Контрагент — противоположная сторона. Если наш счёт стоит не там, где ожидаем, лучше остановиться,
		// чем записать платёж в другую сторону.
		switch t.Direction {
		case "DEBIT":
			if rt.PayerAccount != account {
				return nil, fmt.Errorf("operation %d: debit, but payer account is not %s", i+1, account)
			}
			op.Outgoing = true
			op.CounterAccount, op.CounterINN, op.CounterName = rt.PayeeAccount, rt.PayeeInn, oneLine(rt.PayeeName)
			op.BIK, op.BankName = rt.PayeeBankBic, oneLine(rt.PayeeBankName)
			st.DebitCount++
			st.DebitTotal += amount
		case "CREDIT":
			if rt.PayeeAccount != account {
				return nil, fmt.Errorf("operation %d: credit, but payee account is not %s", i+1, account)
			}
			op.CounterAccount, op.CounterINN, op.CounterName = rt.PayerAccount, rt.PayerInn, oneLine(rt.PayerName)
			op.BIK, op.BankName = rt.PayerBankBic, oneLine(rt.PayerBankName)
			st.CreditCount++
			st.CreditTotal += amount
		default:
			return nil, fmt.Errorf("operation %d: unknown direction %q", i+1, t.Direction)
		}
		if raw, err := json.Marshal(t); err == nil {
			op.Raw = string(raw)
		}
		op.DedupKey = clientbank.DedupKey(op, seen)
		st.Operations = append(st.Operations, op)
	}
	return st, nil
}

var lineBreaks = strings.NewReplacer("\r", "", "\n", "")

// oneLine приводит текст к виду выписки 1С: переносы строк там вырезаются без пробела («годаБез НДС»),
// остальные пробелы схлопываются. Иначе ключ дедупликации API и файла 1С не совпадёт.
func oneLine(s string) string { return clientbank.Collapse(lineBreaks.Replace(s)) }
