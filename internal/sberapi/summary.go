package sberapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// Summary — сводка по рублёвому счёту за один день (ответ /v2/statement/summary): остатки и обороты в копейках.
type Summary struct {
	Opening, Closing int64  // входящий и исходящий остаток
	Debit, Credit    int64  // обороты за день
	ComposedAt       string // время составления сводки как в ответе банка (без часового пояса)
}

type summaryAmount struct {
	Amount json.Number `json:"amount"`
}

// Summary возвращает остатки и обороты счёта за дату. Нужен тот же scope, что и для выписки (GET_STATEMENT_ACCOUNT).
// Берутся поля в рублях (*Rub); дата в будущем и недоступная банку выписка дают *APIError.
func (c *Client) Summary(ctx context.Context, account string, date time.Time) (Summary, error) {
	q := url.Values{"accountNumber": {account}, "statementDate": {date.Format("2006-01-02")}}
	body, err := c.get(ctx, "/fintech/api/v2/statement/summary", q)
	if err != nil {
		return Summary{}, err
	}
	var r struct {
		OpeningBalanceRub summaryAmount `json:"openingBalanceRub"`
		ClosingBalanceRub summaryAmount `json:"closingBalanceRub"`
		DebitTurnoverRub  summaryAmount `json:"debitTurnoverRub"`
		CreditTurnoverRub summaryAmount `json:"creditTurnoverRub"`
		ComposedDateTime  string        `json:"composedDateTime"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Summary{}, fmt.Errorf("sber api: unreadable summary: %w", err)
	}
	var s Summary
	for _, f := range []struct {
		name string
		from summaryAmount
		to   *int64
	}{
		{"openingBalanceRub", r.OpeningBalanceRub, &s.Opening},
		{"closingBalanceRub", r.ClosingBalanceRub, &s.Closing},
		{"debitTurnoverRub", r.DebitTurnoverRub, &s.Debit},
		{"creditTurnoverRub", r.CreditTurnoverRub, &s.Credit},
	} {
		if f.from.Amount == "" {
			return Summary{}, fmt.Errorf("sber api: summary has no %s", f.name)
		}
		v, err := kopecks(f.from.Amount)
		if err != nil {
			return Summary{}, fmt.Errorf("sber api: summary %s: %w", f.name, err)
		}
		*f.to = v
	}
	s.ComposedAt = r.ComposedDateTime
	return s, nil
}
