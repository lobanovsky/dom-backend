// Package sbersync переносит выписки из Sber API в платежи: на каждый счёт и запуск создаётся одна выписка,
// дальше работает обычный импорт выписок (дедупликация по ключу операции, правила определения лицевых счетов).
package sbersync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"dom-backend/internal/model"
	"dom-backend/internal/sberapi"
	"dom-backend/internal/store"
)

// ErrBusy — другой запуск ещё идёт.
var ErrBusy = errors.New("sber sync is already running")

// ErrNotConfigured — токены Sber API ещё не введены.
var ErrNotConfigured = errors.New("sber api is not configured")

type Source interface {
	Transactions(ctx context.Context, account string, date time.Time) ([]sberapi.Transaction, []byte, error)
}

type Statements interface {
	Import(ctx context.Context, fileName string, data []byte, st *model.ParsedStatement) (model.StatementImportResult, error)
}

// Tokens нужен, чтобы не заводить запуски, пока токены не введены.
type Tokens interface {
	Tokens(ctx context.Context) (model.SberTokens, error)
}

type Journal interface {
	SyncAccounts(ctx context.Context) ([]string, error)
	TryLock(ctx context.Context) (func(), bool, error)
	StartRun(ctx context.Context, trigger string, from, to model.Date) (int64, error)
	FinishRun(ctx context.Context, id int64, accounts, incoming, outgoing, skipped int, runErr string) error
}

// Result — итог запуска. Ошибки по отдельным счетам не прерывают остальные и попадают в Errors.
type Result struct {
	RunID    int64    `json:"run_id"`
	Accounts int      `json:"accounts"`
	Incoming int      `json:"incoming"`
	Outgoing int      `json:"outgoing"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors"`
}

type Syncer struct {
	Source     Source
	Statements Statements
	Journal    Journal
	Tokens     Tokens
	Log        *slog.Logger
}

// run — начатый запуск: блокировка взята, запись в журнале создана. Освобождается в exec.
type run struct {
	s        *Syncer
	id       int64
	trigger  string
	from, to time.Time
	unlock   func()
}

// begin проверяет, что токены введены и другой запуск не идёт, берёт блокировку и заводит запись в журнале.
func (s *Syncer) begin(ctx context.Context, trigger string, from, to time.Time) (*run, error) {
	if _, err := s.Tokens.Tokens(ctx); errors.Is(err, store.ErrNotFound) {
		return nil, ErrNotConfigured
	} else if err != nil {
		return nil, err
	}
	unlock, ok, err := s.Journal.TryLock(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrBusy
	}
	id, err := s.Journal.StartRun(ctx, trigger, model.Date{Time: from}, model.Date{Time: to})
	if err != nil {
		unlock()
		return nil, err
	}
	return &run{s: s, id: id, trigger: trigger, from: from, to: to, unlock: unlock}, nil
}

// Sync получает выписки всех счетов за дни from..to (включительно) и ждёт окончания.
func (s *Syncer) Sync(ctx context.Context, trigger string, from, to time.Time) (Result, error) {
	r, err := s.begin(ctx, trigger, from, to)
	if err != nil {
		return Result{Errors: []string{}}, err
	}
	return r.exec(ctx)
}

// Start запускает получение в фоне (долгие периоды не укладываются в таймаут HTTP-запроса) и возвращает номер запуска.
// Ход и итог видны в журнале запусков. ctx — время жизни сервера, а не запроса.
func (s *Syncer) Start(ctx context.Context, trigger string, from, to time.Time) (int64, error) {
	r, err := s.begin(ctx, trigger, from, to)
	if err != nil {
		return 0, err
	}
	go func() { _, _ = r.exec(ctx) }()
	return r.id, nil
}

func (r *run) exec(ctx context.Context) (Result, error) {
	s := r.s
	defer r.unlock()
	res := Result{RunID: r.id, Errors: []string{}}
	accounts, err := s.Journal.SyncAccounts(ctx)
	if err != nil {
		_ = s.Journal.FinishRun(context.WithoutCancel(ctx), r.id, 0, 0, 0, 0, err.Error())
		return res, err
	}
	for _, acc := range accounts {
		res.Accounts++
		in, out, skipped, err := s.syncAccount(ctx, acc, r.from, r.to)
		res.Incoming += in
		res.Outgoing += out
		res.Skipped += skipped
		if err != nil {
			s.Log.Error("sber sync account failed", "account", acc, "err", err)
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", acc, err))
			var authErr *sberapi.AuthError
			if errors.As(err, &authErr) {
				res.Errors = append(res.Errors, "stopped: the bank rejected the tokens") // остальные счета получили бы ту же ошибку
				break
			}
		}
	}
	s.Log.Info("sber sync", "run", r.id, "trigger", r.trigger, "accounts", res.Accounts,
		"incoming", res.Incoming, "outgoing", res.Outgoing, "skipped", res.Skipped, "errors", len(res.Errors))
	_ = s.Journal.FinishRun(context.WithoutCancel(ctx), r.id, res.Accounts, res.Incoming, res.Outgoing, res.Skipped, strings.Join(res.Errors, "; "))
	if errors.Is(ctx.Err(), context.Canceled) {
		return res, ctx.Err()
	}
	return res, nil
}

func (s *Syncer) syncAccount(ctx context.Context, account string, from, to time.Time) (in, out, skipped int, err error) {
	var all []sberapi.Transaction
	var raw []byte
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		txs, body, err := s.Source.Transactions(ctx, account, d)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("%s: %w", d.Format("2006-01-02"), err)
		}
		all = append(all, txs...)
		raw = append(raw, body...)
	}
	if len(all) == 0 {
		return 0, 0, 0, nil
	}
	st, err := sberapi.Statement(account, from, to, all)
	if err != nil {
		return 0, 0, 0, err
	}
	name := fmt.Sprintf("sberapi-%s-%s_%s.json", account, from.Format("2006-01-02"), to.Format("2006-01-02"))
	r, err := s.Statements.Import(ctx, name, raw, st)
	var exists *store.StatementExistsError
	var dup *store.StatementAllDuplicatesError
	switch {
	case errors.As(err, &exists):
		return 0, 0, len(all), nil // тот же ответ банка уже загружен
	case errors.As(err, &dup):
		return 0, 0, dup.Total, nil // все операции уже есть (из файла 1С или прошлого запуска)
	case err != nil:
		return 0, 0, 0, err
	}
	return r.Incoming, r.Outgoing, r.SkippedDuplicates, nil
}

// Run запускает опрос по расписанию, пока не отменён ctx. days — сколько последних дней (включая сегодня) запрашивать.
func (s *Syncer) Run(ctx context.Context, interval time.Duration, days int) {
	if interval <= 0 {
		return
	}
	s.Log.Info("sber sync scheduler started", "interval", interval.String(), "days", days)
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		now := time.Now()
		to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		_, err := s.Sync(ctx, "schedule", to.AddDate(0, 0, -(days-1)), to)
		switch {
		case errors.Is(err, ErrBusy):
			s.Log.Info("sber sync skipped: another run is in progress")
		case errors.Is(err, ErrNotConfigured):
			s.Log.Debug("sber sync skipped: tokens are not set")
		case err != nil && !errors.Is(err, context.Canceled):
			s.Log.Error("sber sync failed", "err", err)
		}
		timer.Reset(interval)
	}
}
