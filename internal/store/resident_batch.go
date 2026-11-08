package store

import (
	"context"
	"dom-backend/internal/model"
	"fmt"
	"github.com/jackc/pgx/v5"
	"sort"
)

type ResidentReadingError struct {
	MeterID int64
	Err     error
}

func (e *ResidentReadingError) Error() string { return fmt.Sprintf("meter %d: %s", e.MeterID, e.Err) }
func (e *ResidentReadingError) Unwrap() error { return e.Err }

// SubmitReadings сохраняет весь заполненный пакет помещения либо не сохраняет ничего.
func (s *Residents) SubmitReadings(ctx context.Context, a model.ResidentAccount, premises int64, inputs []model.ResidentReadingInput) ([]model.MeterReading, error) {
	if len(inputs) == 0 || len(inputs) > 100 {
		return nil, invalid("readings: expected 1 to 100 readings")
	}
	items := append([]model.ResidentReadingInput(nil), inputs...)
	sort.Slice(items, func(i, j int) bool { return items[i].MeterID < items[j].MeterID })
	ids := make([]int64, len(items))
	for i, in := range items {
		if i > 0 && items[i-1].MeterID == in.MeterID {
			return nil, invalid("readings: duplicate meter")
		}
		if _, err := in.Reading(); err != nil {
			return nil, &ResidentReadingError{in.MeterID, err}
		}
		ids[i] = in.MeterID
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(ctx, tx)
	// Единый порядок блокировок защищает от параллельных отправок совладельцами.
	rows, err := tx.Query(ctx, `SELECT m.id FROM meters m JOIN premises p ON p.id=m.premises_id WHERE m.id=ANY($2::bigint[]) AND p.id=$3 AND m.deleted_at IS NULL AND `+residentMeterActive+` AND `+residentOwns+` ORDER BY m.id FOR UPDATE OF m`, a.PersonID, ids, premises)
	if err != nil {
		return nil, mapErr(err)
	}
	locked, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, mapErr(err)
	}
	if len(locked) != len(ids) {
		return nil, ErrNotFound
	}
	var building int64
	if err = tx.QueryRow(ctx, `SELECT building_id FROM premises WHERE id=$1`, premises).Scan(&building); err != nil {
		return nil, mapErr(err)
	}
	window, err := lockResidentWindow(ctx, tx, building)
	if err != nil {
		return nil, err
	}
	saved := make([]int64, 0, len(items))
	for _, in := range items {
		r, _ := in.Reading()
		id, err := writeResidentReading(ctx, tx, a, r, in.Revision, &premises, &window)
		if err != nil {
			return nil, &ResidentReadingError{in.MeterID, err}
		}
		saved = append(saved, id)
	}
	result, err := collect[model.MeterReading](tx.Query(ctx, readingSelect+` WHERE r.id=ANY($1::bigint[]) ORDER BY r.meter_id`, saved))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, mapErr(err)
	}
	return result, nil
}
