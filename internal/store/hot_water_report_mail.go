package store

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"dom-backend/internal/model"
	"github.com/jackc/pgx/v5"
)

// SendHotWaterReport сериализует отправки за месяц и сохраняет отметку после подтверждения SMTP.
func (s *Water) SendHotWaterReport(ctx context.Context, period model.Date, send func(context.Context) error) (time.Time, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer rollback(ctx, tx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('hot water report:' || $1::date::text, 0))`, period); err != nil {
		return time.Time{}, err
	}
	var sentAt time.Time
	err = tx.QueryRow(ctx, `SELECT sent_at FROM hot_water_report_sends WHERE period=$1`, period).Scan(&sentAt)
	if err == nil {
		return sentAt, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, err
	}
	if err := send(ctx); err != nil {
		slog.Error("не удалось отправить справку ГВС", "period", period.Format("2006-01"), "err", err)
		return time.Time{}, &Error{Kind: ErrIntegration, Msg: "Не удалось отправить справку по почте."}
	}
	if err := tx.QueryRow(ctx, `INSERT INTO hot_water_report_sends (period) VALUES ($1) RETURNING sent_at`, period).Scan(&sentAt); err != nil {
		return time.Time{}, err
	}
	return sentAt, tx.Commit(ctx)
}
