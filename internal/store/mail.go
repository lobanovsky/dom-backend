package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

// mailLockKey — ключ advisory-блокировки: два запуска забора почты не идут одновременно.
const mailLockKey = 7340002

// Mail — журнал запусков забора реестров из почты и список уже разобранных писем.
type Mail struct{ pool *pgxpool.Pool }

func NewMail(pool *pgxpool.Pool) *Mail { return &Mail{pool} }

// TryLock берёт блокировку запуска; вторая попытка до unlock возвращает false.
func (s *Mail) TryLock(ctx context.Context) (unlock func(), ok bool, err error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, mailLockKey).Scan(&ok); err != nil || !ok {
		conn.Release()
		return nil, false, err
	}
	return func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, mailLockKey)
		conn.Release()
	}, true, nil
}

func (s *Mail) StartRun(ctx context.Context, trigger string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO mail_sync_runs (trigger) VALUES ($1) RETURNING id`, trigger).Scan(&id)
	return id, mapErr(err)
}

// FinishRun записывает итог; details — JSON-массив результатов по файлам.
func (s *Mail) FinishRun(ctx context.Context, id int64, r model.MailRun) error {
	if len(r.Details) == 0 {
		r.Details = []byte("[]")
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE mail_sync_runs SET finished_at = now(), messages = $2, files = $3, imported = $4, payments = $5,
		        duplicates = $6, failed = $7, error = $8, details = $9 WHERE id = $1`,
		id, r.Messages, r.Files, r.Imported, r.Payments, r.Duplicates, r.Failed, r.Error, r.Details)
	return mapErr(err)
}

func (s *Mail) Runs(ctx context.Context, limit int) ([]model.MailRun, error) {
	return collect[model.MailRun](s.pool.Query(ctx,
		`SELECT id, started_at, finished_at, trigger, messages, files, imported, payments, duplicates, failed, error, details
		 FROM mail_sync_runs ORDER BY started_at DESC, id DESC LIMIT $1`, limit))
}

// AbortUnfinished помечает запуски, прерванные остановкой сервера: иначе они навсегда остались бы «идущими».
func (s *Mail) AbortUnfinished(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE mail_sync_runs SET finished_at = now(), error = 'interrupted: server restarted' WHERE finished_at IS NULL`)
	return mapErr(err)
}

// Processed — UID писем папки, которые сервер уже разобрал (в рамках текущего UIDVALIDITY).
func (s *Mail) Processed(ctx context.Context, folder string, uidValidity uint32) (map[uint32]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT uid FROM mail_messages WHERE folder = $1 AND uid_validity = $2`, folder, uidValidity)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := map[uint32]bool{}
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, mapErr(err)
		}
		out[uint32(uid)] = true
	}
	return out, mapErr(rows.Err())
}

func (s *Mail) MarkProcessed(ctx context.Context, m model.MailMessage) error {
	var received any
	if !m.ReceivedAt.IsZero() {
		received = m.ReceivedAt
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO mail_messages (folder, uid_validity, uid, message_id, from_addr, subject, received_at, files)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (folder, uid_validity, uid) DO NOTHING`,
		m.Folder, m.UIDValidity, m.UID, m.MessageID, m.From, m.Subject, received, m.Files)
	return mapErr(err)
}
