package sbersync

import (
	"context"
	"errors"
	"net/http"
	"time"

	"dom-backend/internal/model"
	"dom-backend/internal/sberapi"
)

// Balance — остаток и обороты счёта за день из Sber API. При ошибке по счёту заполнены только Number и Error.
type Balance struct {
	Number     string      `json:"number"`
	Date       *model.Date `json:"date"`        // день, на конец которого остаток
	Opening    *float64    `json:"opening"`     // входящий остаток
	Closing    *float64    `json:"closing"`     // исходящий остаток
	Debit      *float64    `json:"debit"`       // списано за день
	Credit     *float64    `json:"credit"`      // поступило за день
	ComposedAt string      `json:"composed_at"` // время составления сводки банком
	Error      string      `json:"error,omitempty"`
}

// moscow — банк считает операционные дни по московскому времени.
var moscow = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/Moscow"); err == nil {
		return loc
	}
	return time.FixedZone("MSK", 3*3600)
}()

func rub(kop int64) *float64 {
	v := float64(kop) / 100
	return &v
}

// Balances запрашивает у банка остатки по действующим счетам Сбера: за сегодня, а если банк сводку за сегодня не отдаёт
// (400/404), за вчера. Ошибка одного счёта не мешает остальным; сбой авторизации прерывает всё.
func (s *Syncer) Balances(ctx context.Context) ([]Balance, error) {
	if _, err := s.Tokens.Tokens(ctx); err != nil {
		return nil, ErrNotConfigured
	}
	accounts, err := s.Journal.SyncAccounts(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().In(moscow)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	out := make([]Balance, 0, len(accounts))
	for _, account := range accounts {
		b := Balance{Number: account}
		day := today
		sum, err := s.Source.Summary(ctx, account, day)
		var api *sberapi.APIError
		if errors.As(err, &api) && (api.Status == http.StatusBadRequest || api.Status == http.StatusNotFound) {
			day = today.AddDate(0, 0, -1)
			sum, err = s.Source.Summary(ctx, account, day)
		}
		var auth *sberapi.AuthError
		if errors.As(err, &auth) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		if err != nil {
			b.Error = err.Error()
			s.Log.Warn("sber balance failed", "account", account, "error", err)
			out = append(out, b)
			continue
		}
		b.Date = &model.Date{Time: day}
		b.Opening, b.Closing, b.Debit, b.Credit, b.ComposedAt = rub(sum.Opening), rub(sum.Closing), rub(sum.Debit), rub(sum.Credit), sum.ComposedAt
		out = append(out, b)
	}
	return out, nil
}
