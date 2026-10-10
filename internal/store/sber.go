package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

// sberLockKey — ключ advisory-блокировки: два запуска получения выписок не идут одновременно.
const sberLockKey = 7340001

// Sber хранит токены Sber API, журнал запусков и список счетов для опроса.
type Sber struct{ pool *pgxpool.Pool }

func NewSber(pool *pgxpool.Pool) *Sber { return &Sber{pool} }

// Tokens возвращает сохранённую пару токенов; ErrNotFound, если токены ещё не вводили.
func (s *Sber) Tokens(ctx context.Context) (model.SberTokens, error) {
	var t model.SberTokens
	err := s.pool.QueryRow(ctx, `SELECT access_token, refresh_token, access_expires_at FROM sber_tokens WHERE id = 1`).
		Scan(&t.AccessToken, &t.RefreshToken, &t.AccessExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, &Error{Kind: ErrNotFound, Msg: "sber tokens are not set"}
	}
	return t, mapErr(err)
}

func (s *Sber) SaveTokens(ctx context.Context, t model.SberTokens) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sber_tokens (id, access_token, refresh_token, access_expires_at) VALUES (1, $1, $2, $3)
		 ON CONFLICT (id) DO UPDATE SET access_token = $1, refresh_token = $2, access_expires_at = $3, updated_at = now()`,
		t.AccessToken, t.RefreshToken, t.AccessExpiresAt)
	return mapErr(err)
}

// TokensUpdatedAt нужен статусу: когда токены обновлялись в последний раз (nil — ещё не вводили).
func (s *Sber) TokensUpdatedAt(ctx context.Context) (*time.Time, error) {
	var at time.Time
	err := s.pool.QueryRow(ctx, `SELECT updated_at FROM sber_tokens WHERE id = 1`).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &at, mapErr(err)
}

// TryLock берёт блокировку запуска; вторая попытка до Unlock возвращает false.
// Блокировка держится на отдельном соединении, поэтому Unlock нужно вызвать у того же значения.
func (s *Sber) TryLock(ctx context.Context) (unlock func(), ok bool, err error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, sberLockKey).Scan(&ok); err != nil || !ok {
		conn.Release()
		return nil, false, err
	}
	return func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, sberLockKey)
		conn.Release()
	}, true, nil
}

func (s *Sber) StartRun(ctx context.Context, trigger string, from, to model.Date) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO sber_sync_runs (trigger, date_from, date_to) VALUES ($1, $2, $3) RETURNING id`,
		trigger, from, to).Scan(&id)
	return id, mapErr(err)
}

func (s *Sber) FinishRun(ctx context.Context, id int64, accounts, incoming, outgoing, skipped int, runErr string) error {
	var e *string
	if runErr != "" {
		e = &runErr
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE sber_sync_runs SET finished_at = now(), accounts = $2, incoming = $3, outgoing = $4, skipped = $5, error = $6 WHERE id = $1`,
		id, accounts, incoming, outgoing, skipped, e)
	return mapErr(err)
}

func (s *Sber) Runs(ctx context.Context, limit int) ([]model.SberRun, error) {
	return collect[model.SberRun](s.pool.Query(ctx,
		`SELECT id, started_at, finished_at, trigger, date_from, date_to, accounts, incoming, outgoing, skipped, error
		 FROM sber_sync_runs ORDER BY started_at DESC, id DESC LIMIT $1`, limit))
}

// LastFinishedOK возвращает время последнего успешного запуска (nil, если не было).
func (s *Sber) LastFinishedOK(ctx context.Context) (*time.Time, error) {
	var at *time.Time
	err := s.pool.QueryRow(ctx, `SELECT MAX(finished_at) FROM sber_sync_runs WHERE finished_at IS NOT NULL AND error IS NULL`).Scan(&at)
	return at, mapErr(err)
}

// SyncAccounts — номера действующих счетов, которые опрашиваем: счета Сбербанка или без указанного БИК.
func (s *Sber) SyncAccounts(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT number FROM bank_accounts
		 WHERE deleted_at IS NULL AND valid_from <= CURRENT_DATE AND (valid_to IS NULL OR valid_to >= CURRENT_DATE)
		   AND (bik IS NULL OR bik = '044525225') ORDER BY number`)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, n)
	}
	return out, mapErr(rows.Err())
}

// AbortUnfinished помечает запуски, прерванные остановкой сервера: иначе они навсегда остались бы «идущими».
func (s *Sber) AbortUnfinished(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE sber_sync_runs SET finished_at = now(), error = 'interrupted: server restarted' WHERE finished_at IS NULL`)
	return mapErr(err)
}
