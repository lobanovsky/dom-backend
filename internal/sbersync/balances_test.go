package sbersync

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"dom-backend/internal/model"
	"dom-backend/internal/sberapi"
)

type balJournal struct {
	Journal
	accounts []string
}

func (j balJournal) SyncAccounts(context.Context) ([]string, error) { return j.accounts, nil }

type balTokens struct{ err error }

func (t balTokens) Tokens(context.Context) (model.SberTokens, error) {
	return model.SberTokens{}, t.err
}

// balSource отвечает по счёту и дню: нет записи — банк не отдаёт сводку (404).
type balSource struct {
	Source
	byAccount map[string]map[int]error // account -> сколько дней назад -> ошибка (nil = есть данные)
	asked     []time.Time
}

func (s *balSource) Summary(_ context.Context, account string, d time.Time) (sberapi.Summary, error) {
	s.asked = append(s.asked, d)
	now := time.Now().In(moscow)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	ago := int(today.Sub(d).Hours() / 24)
	if err, ok := s.byAccount[account][ago]; ok {
		if err != nil {
			return sberapi.Summary{}, err
		}
		return sberapi.Summary{Opening: 100, Closing: 12345, Debit: 5, Credit: 12250, ComposedAt: "2026-10-12T09:15:00"}, nil
	}
	return sberapi.Summary{}, &sberapi.APIError{Status: 404, Message: "DATA_NOT_FOUND"}
}

func newBalSyncer(src Source, tokensErr error, accounts ...string) *Syncer {
	return &Syncer{Source: src, Journal: balJournal{accounts: accounts}, Tokens: balTokens{tokensErr}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestBalancesFallbackAndPerAccountErrors(t *testing.T) {
	src := &balSource{byAccount: map[string]map[int]error{
		"A": {0: nil},                                                         // сегодня есть
		"B": {1: nil},                                                         // сегодня нет (404), вчера есть
		"C": {0: &sberapi.APIError{Status: 403, Message: "ACCESS_EXCEPTION"}}, // нет доступа к счёту
	}}
	got, err := newBalSyncer(src, nil, "A", "B", "C").Balances(context.Background())
	if err != nil || len(got) != 3 {
		t.Fatalf("Balances = %+v, err = %v", got, err)
	}
	if got[0].Error != "" || got[0].Closing == nil || *got[0].Closing != 123.45 || *got[0].Credit != 122.5 || got[0].ComposedAt == "" {
		t.Errorf("A: %+v", got[0])
	}
	if got[1].Error != "" || got[1].Date == nil || !got[1].Date.Time.Before(got[0].Date.Time) {
		t.Errorf("B must fall back to yesterday: %+v", got[1])
	}
	if got[2].Error == "" || got[2].Closing != nil || got[2].Number != "C" {
		t.Errorf("C must carry its own error: %+v", got[2])
	}
}

func TestBalancesStopsOnAuthErrorAndWithoutTokens(t *testing.T) {
	src := &balSource{byAccount: map[string]map[int]error{"A": {0: &sberapi.AuthError{APIError: sberapi.APIError{Status: 401}}}}}
	if _, err := newBalSyncer(src, nil, "A", "B").Balances(context.Background()); err == nil {
		t.Error("auth error must abort")
	}
	if _, err := newBalSyncer(src, errors.New("none"), "A").Balances(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("without tokens: err = %v, want ErrNotConfigured", err)
	}
}
