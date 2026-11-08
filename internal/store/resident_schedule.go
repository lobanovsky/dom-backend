package store

import (
	"context"
	"dom-backend/internal/model"
	"github.com/jackc/pgx/v5"
)

type residentQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadResidentSettings(ctx context.Context, q residentQuerier, ids []int64) (map[int64]model.ResidentScheduleSettings, error) {
	result := map[int64]model.ResidentScheduleSettings{}
	if len(ids) == 0 {
		return result, nil
	}
	schedules, err := collect[model.ResidentSchedule](q.Query(ctx, `SELECT b.id AS building_id,COALESCE(s.enabled,false) AS enabled,COALESCE(s.opens_day,17) AS opens_day,COALESCE(s.closes_day,22) AS closes_day FROM buildings b LEFT JOIN resident_reading_schedules s ON s.building_id=b.id WHERE b.id=ANY($1::bigint[]) AND b.deleted_at IS NULL`, ids))
	if err != nil {
		return nil, err
	}
	overrides, err := collect[model.ResidentWindow](q.Query(ctx, `SELECT building_id,period,opens_on,closes_on,enabled FROM resident_reading_overrides WHERE building_id=ANY($1::bigint[]) ORDER BY period DESC`, ids))
	if err != nil {
		return nil, err
	}
	var today model.Date
	if err = q.QueryRow(ctx, `SELECT (now() AT TIME ZONE 'Europe/Moscow')::date`).Scan(&today); err != nil {
		return nil, mapErr(err)
	}
	for _, schedule := range schedules {
		items := []model.ResidentWindow{}
		for _, w := range overrides {
			if w.BuildingID == schedule.BuildingID {
				w.ServerDate = today
				items = append(items, w)
			}
		}
		result[schedule.BuildingID] = model.ResidentScheduleSettings{ResidentSchedule: schedule, Overrides: items, CurrentWindow: model.ResolveResidentWindow(schedule, items, today)}
	}
	return result, nil
}

func (s *Residents) Schedule(ctx context.Context, building int64) (model.ResidentScheduleSettings, error) {
	settings, err := loadResidentSettings(ctx, s.pool, []int64{building})
	if err != nil {
		return model.ResidentScheduleSettings{}, err
	}
	v, ok := settings[building]
	if !ok {
		return v, ErrNotFound
	}
	return v, nil
}

func (s *Residents) SetSchedule(ctx context.Context, in model.ResidentSchedule) (model.ResidentScheduleSettings, error) {
	if err := in.Validate(); err != nil {
		return model.ResidentScheduleSettings{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.ResidentScheduleSettings{}, err
	}
	defer rollback(ctx, tx)
	var id int64
	if err = tx.QueryRow(ctx, `SELECT id FROM buildings WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, in.BuildingID).Scan(&id); err != nil {
		return model.ResidentScheduleSettings{}, mapErr(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO resident_reading_schedules(building_id,enabled,opens_day,closes_day) VALUES($1,$2,$3,$4) ON CONFLICT(building_id) DO UPDATE SET enabled=excluded.enabled,opens_day=excluded.opens_day,closes_day=excluded.closes_day,updated_at=now()`, in.BuildingID, in.Enabled, in.OpensDay, in.ClosesDay)
	if err != nil {
		return model.ResidentScheduleSettings{}, mapErr(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return model.ResidentScheduleSettings{}, mapErr(err)
	}
	return s.Schedule(ctx, in.BuildingID)
}

func (s *Residents) SetWindow(ctx context.Context, w model.ResidentWindow) error {
	if w.Period.IsZero() || w.Period.Day() != 1 || w.OpensOn.IsZero() || w.ClosesOn.IsZero() || w.ClosesOn.Before(w.OpensOn.Time) {
		return invalid("invalid reading window")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var id int64
	if err = tx.QueryRow(ctx, `SELECT id FROM buildings WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, w.BuildingID).Scan(&id); err != nil {
		return mapErr(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO resident_reading_overrides(building_id,period,opens_on,closes_on,enabled) VALUES($1,$2,$3,$4,$5) ON CONFLICT(building_id,period) DO UPDATE SET opens_on=excluded.opens_on,closes_on=excluded.closes_on,enabled=excluded.enabled,updated_at=now()`, w.BuildingID, w.Period, w.OpensOn, w.ClosesOn, w.Enabled)
	if err != nil {
		return mapErr(err)
	}
	return tx.Commit(ctx)
}
func (s *Residents) DeleteWindow(ctx context.Context, building int64, period model.Date) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var id int64
	if err = tx.QueryRow(ctx, `SELECT id FROM buildings WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, building).Scan(&id); err != nil {
		return mapErr(err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM resident_reading_overrides WHERE building_id=$1 AND period=$2`, building, period); err != nil {
		return mapErr(err)
	}
	return tx.Commit(ctx)
}
func (s *Residents) Window(ctx context.Context, building int64) (*model.ResidentWindow, error) {
	settings, err := s.Schedule(ctx, building)
	if err != nil {
		return nil, err
	}
	return &settings.CurrentWindow, nil
}

// lockResidentWindow синхронизирует приём с изменением расписания администратором.
func lockResidentWindow(ctx context.Context, tx pgx.Tx, building int64) (model.ResidentWindow, error) {
	var id int64
	if err := tx.QueryRow(ctx, `SELECT id FROM buildings WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, building).Scan(&id); err != nil {
		return model.ResidentWindow{}, mapErr(err)
	}
	settings, err := loadResidentSettings(ctx, tx, []int64{building})
	if err != nil {
		return model.ResidentWindow{}, err
	}
	w := settings[building].CurrentWindow
	if !w.IsOpen(w.ServerDate) {
		return w, invalid("reading window is closed")
	}
	return w, nil
}
