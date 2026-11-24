package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

// Расход считается от предыдущего действующего показания (для первого — от начальных показаний счётчика) с учётом коэффициента.
const readingSelect = `SELECT r.id, r.meter_id, r.reading_date, r.period, r.t1, r.t2, r.t3, r.source, r.comment,
	(r.t1 - COALESCE(prev.t1, m.initial_t1)) * m.coefficient AS delta_t1,
	CASE WHEN r.t2 IS NULL THEN NULL ELSE (r.t2 - COALESCE(prev.t2, m.initial_t2)) * m.coefficient END AS delta_t2,
	CASE WHEN r.t3 IS NULL THEN NULL ELSE (r.t3 - COALESCE(prev.t3, m.initial_t3)) * m.coefficient END AS delta_t3,
	r.created_at, r.updated_at, r.deleted_at
	FROM meter_readings r
	JOIN meters m ON m.id = r.meter_id
	LEFT JOIN LATERAL (
		SELECT p.t1, p.t2, p.t3 FROM meter_readings p
		WHERE p.meter_id = r.meter_id AND p.deleted_at IS NULL AND p.reading_date < r.reading_date
		ORDER BY p.reading_date DESC LIMIT 1
	) prev ON true`

type MeterReadings struct{ pool *pgxpool.Pool }

func NewMeterReadings(pool *pgxpool.Pool) *MeterReadings { return &MeterReadings{pool} }

// ApplyDefaultReadings adds configured average monthly consumption to the latest earlier reading
// (or to the meter's initial value) for active manual meters that have no reading this month.
func (s *MeterReadings) ApplyDefaultReadings(ctx context.Context) (model.MeterDefaultReadingsResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.MeterDefaultReadingsResult{}, mapErr(err)
	}
	defer rollback(ctx, tx)
	var today, period model.Date
	if err := tx.QueryRow(ctx, `SELECT (now() AT TIME ZONE 'Europe/Moscow')::date, date_trunc('month', now() AT TIME ZONE 'Europe/Moscow')::date`).Scan(&today, &period); err != nil {
		return model.MeterDefaultReadingsResult{}, mapErr(err)
	}
	var unconfigured int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM meters m
		WHERE m.deleted_at IS NULL AND m.manual_reading_required
		  AND m.installed_at <= $2 AND (m.removed_at IS NULL OR m.removed_at >= $2)
		  AND NOT EXISTS (SELECT 1 FROM meter_readings r WHERE r.meter_id=m.id AND r.deleted_at IS NULL AND r.period=$1)
		  AND (m.default_t1 IS NULL OR (m.tariffs >= 2 AND m.default_t2 IS NULL) OR (m.tariffs >= 3 AND m.default_t3 IS NULL))`, period, today).Scan(&unconfigured); err != nil {
		return model.MeterDefaultReadingsResult{}, mapErr(err)
	}
	rows, err := tx.Query(ctx, `
		SELECT m.id, m.tariffs,
		       m.default_t1, m.default_t2, m.default_t3,
		       COALESCE(prev.t1, m.initial_t1), COALESCE(prev.t2, m.initial_t2), COALESCE(prev.t3, m.initial_t3)
		FROM meters m
		LEFT JOIN LATERAL (
			SELECT r.t1, r.t2, r.t3 FROM meter_readings r
			WHERE r.meter_id=m.id AND r.deleted_at IS NULL AND r.period < $1
			ORDER BY r.period DESC, r.reading_date DESC LIMIT 1
		) prev ON true
		WHERE m.deleted_at IS NULL AND m.manual_reading_required
		  AND m.installed_at <= $2 AND (m.removed_at IS NULL OR m.removed_at >= $2)
		  AND m.default_t1 IS NOT NULL
		  AND (m.tariffs < 2 OR m.default_t2 IS NOT NULL)
		  AND (m.tariffs < 3 OR m.default_t3 IS NOT NULL)
		  AND NOT EXISTS (SELECT 1 FROM meter_readings r WHERE r.meter_id=m.id AND r.deleted_at IS NULL AND r.period=$1)
		ORDER BY m.id FOR UPDATE OF m`, period, today)
	if err != nil {
		return model.MeterDefaultReadingsResult{}, mapErr(err)
	}
	type candidate struct {
		id       int64
		tariffs  int
		defaults [3]*float64
		previous [3]float64
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.tariffs,
			&c.defaults[0], &c.defaults[1], &c.defaults[2],
			&c.previous[0], &c.previous[1], &c.previous[2]); err != nil {
			rows.Close()
			return model.MeterDefaultReadingsResult{}, mapErr(err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return model.MeterDefaultReadingsResult{}, mapErr(err)
	}
	rows.Close()
	result := model.MeterDefaultReadingsResult{Skipped: len(candidates), Unconfigured: unconfigured}
	for _, c := range candidates {
		values := [3]float64{}
		for i := 0; i < c.tariffs; i++ {
			values[i] = c.previous[i] + *c.defaults[i]
		}
		comment := "Подставлено среднее значение по умолчанию"
		in := model.MeterReading{MeterID: c.id, ReadingDate: today, Period: period, T1: values[0], Source: "manual", Comment: &comment}
		if c.tariffs >= 2 {
			in.T2 = &values[1]
		}
		if c.tariffs >= 3 {
			in.T3 = &values[2]
		}
		in.SetDefaults()
		if err := checkReading(ctx, tx, in, 0, false); err != nil {
			if errors.Is(err, ErrInvalid) {
				continue
			}
			return model.MeterDefaultReadingsResult{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO meter_readings (meter_id, reading_date, period, t1, t2, t3, source, comment) VALUES ($1,$2,$3,$4,$5,$6,'manual',$7)`, in.MeterID, in.ReadingDate, in.Period, in.T1, in.T2, in.T3, in.Comment); err != nil {
			return model.MeterDefaultReadingsResult{}, mapErr(err)
		}
		result.Applied++
		result.Skipped--
	}
	if err := tx.Commit(ctx); err != nil {
		return model.MeterDefaultReadingsResult{}, mapErr(err)
	}
	return result, nil
}

func (s *MeterReadings) List(ctx context.Context, f model.MeterReadingFilter, limit, offset int) ([]model.MeterReading, error) {
	return collect[model.MeterReading](s.pool.Query(ctx,
		readingSelect+`
		 WHERE ($1::bigint IS NULL OR r.meter_id = $1)
		   AND ($2::date IS NULL OR r.reading_date >= $2)
		   AND ($3::date IS NULL OR r.reading_date <= $3)
		   AND ($4::date IS NULL OR r.period = $4)
		   AND (r.deleted_at IS NOT NULL) = $5
		 ORDER BY r.reading_date DESC, r.id DESC LIMIT $6 OFFSET $7`,
		f.MeterID, f.DateFrom, f.DateTo, f.Period, f.Deleted, limit, offset))
}

func (s *MeterReadings) Get(ctx context.Context, id int64) (model.MeterReading, error) {
	return one[model.MeterReading](s.pool.Query(ctx, readingSelect+` WHERE r.id = $1`, id))
}

func (s *MeterReadings) Create(ctx context.Context, in model.MeterReading) (model.MeterReading, error) {
	in.SetDefaults()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.MeterReading{}, mapErr(err)
	}
	defer rollback(ctx, tx)
	if err := checkReading(ctx, tx, in, 0, true); err != nil {
		return model.MeterReading{}, err
	}
	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO meter_readings (meter_id, reading_date, period, t1, t2, t3, source, comment) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		in.MeterID, in.ReadingDate, in.Period, in.T1, in.T2, in.T3, in.Source, in.Comment).Scan(&id)
	if err != nil {
		return model.MeterReading{}, mapErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.MeterReading{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

func (s *MeterReadings) Update(ctx context.Context, id int64, in model.MeterReading) (model.MeterReading, error) {
	in.SetDefaults()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.MeterReading{}, mapErr(err)
	}
	defer rollback(ctx, tx)
	if err := checkReading(ctx, tx, in, id, true); err != nil {
		return model.MeterReading{}, err
	}
	tag, err := tx.Exec(ctx,
		`UPDATE meter_readings SET meter_id = $2, reading_date = $3, period = $4, t1 = $5, t2 = $6, t3 = $7, source = $8, comment = $9, import_run_id = NULL, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`,
		id, in.MeterID, in.ReadingDate, in.Period, in.T1, in.T2, in.T3, in.Source, in.Comment)
	if err := checkDeleted(tag, err); err != nil {
		return model.MeterReading{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.MeterReading{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

// Restore возвращает удалённое показание, если оно по-прежнему согласуется с соседними.
func (s *MeterReadings) Restore(ctx context.Context, id int64) (model.MeterReading, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.MeterReading{}, mapErr(err)
	}
	defer rollback(ctx, tx)
	var fromUndoneImport bool
	err = tx.QueryRow(ctx, `SELECT COALESCE(i.undone_at IS NOT NULL, false)
		FROM meter_readings r LEFT JOIN meter_reading_import_runs i ON i.id=r.import_run_id
		WHERE r.id=$1 AND r.deleted_at IS NOT NULL`, id).Scan(&fromUndoneImport)
	if err == pgx.ErrNoRows {
		return model.MeterReading{}, &Error{ErrNotFound, "not found"}
	}
	if err != nil {
		return model.MeterReading{}, mapErr(err)
	}
	if fromUndoneImport {
		return model.MeterReading{}, invalid("cannot restore a reading from an undone import")
	}
	cur, err := one[model.MeterReading](tx.Query(ctx, readingSelect+` WHERE r.id = $1 AND r.deleted_at IS NOT NULL`, id))
	if err != nil {
		return model.MeterReading{}, err
	}
	if err := checkReading(ctx, tx, cur, id, true); err != nil {
		return model.MeterReading{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE meter_readings SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	if err := checkDeleted(tag, err); err != nil {
		return model.MeterReading{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.MeterReading{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

func (s *MeterReadings) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "meter_readings", id)
}

// checkReading проверяет тарифы, период работы и порядок показаний. allowDecrease разрешает администратору
// исправить ошибочно завышенное значение; жительский ввод и импорт по-прежнему не могут уменьшать показания.
// excludeID — сама проверяемая запись при правке. Счётчик блокируется до конца транзакции.
func checkReading(ctx context.Context, tx pgx.Tx, in model.MeterReading, excludeID int64, allowDecrease bool) error {
	var (
		tariffs                      int
		installed                    model.Date
		removed                      *model.Date
		initial1, initial2, initial3 float64
	)
	err := tx.QueryRow(ctx,
		`SELECT tariffs, installed_at, removed_at, initial_t1, initial_t2, initial_t3 FROM meters WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		in.MeterID).Scan(&tariffs, &installed, &removed, &initial1, &initial2, &initial3)
	if err != nil {
		if err == pgx.ErrNoRows {
			return invalid("meter_id: meter not found")
		}
		return mapErr(err)
	}
	if tariffs >= 2 && in.T2 == nil {
		return invalid("t2: is required for this meter")
	}
	if tariffs < 2 && in.T2 != nil {
		return invalid("t2: must be empty for this meter")
	}
	if tariffs >= 3 && in.T3 == nil {
		return invalid("t3: is required for this meter")
	}
	if tariffs < 3 && in.T3 != nil {
		return invalid("t3: must be empty for this meter")
	}
	if in.ReadingDate.Before(installed.Time) {
		return invalid("reading_date: must not be before the meter installation date")
	}
	if removed != nil && in.ReadingDate.After(removed.Time) {
		return invalid("reading_date: must not be after the meter removal date")
	}

	neighbour := func(op, order string) (vals [3]*float64, found bool, err error) {
		err = tx.QueryRow(ctx,
			`SELECT t1, t2, t3 FROM meter_readings
			 WHERE meter_id = $1 AND deleted_at IS NULL AND id <> $2 AND reading_date `+op+` $3
			 ORDER BY reading_date `+order+` LIMIT 1`,
			in.MeterID, excludeID, in.ReadingDate).Scan(&vals[0], &vals[1], &vals[2])
		if err == pgx.ErrNoRows {
			return vals, false, nil
		}
		return vals, err == nil, err
	}
	prev, ok, err := neighbour("<", "DESC")
	if err != nil {
		return mapErr(err)
	}
	if !ok {
		prev = [3]*float64{&initial1, &initial2, &initial3}
	}
	next, hasNext, err := neighbour(">", "ASC")
	if err != nil {
		return mapErr(err)
	}
	cur := [3]*float64{&in.T1, in.T2, in.T3}
	for i, name := range []string{"t1", "t2", "t3"} {
		if cur[i] == nil {
			continue
		}
		if !allowDecrease && prev[i] != nil && model.Milli(*cur[i]) < model.Milli(*prev[i]) {
			return invalid(name + ": must not be less than previous reading")
		}
		if hasNext && next[i] != nil && model.Milli(*cur[i]) > model.Milli(*next[i]) {
			return invalid(name + ": must not be greater than next reading")
		}
	}
	return nil
}
