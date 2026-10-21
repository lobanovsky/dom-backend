package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

// Счётчик активен между вводом в эксплуатацию и датой окончания (включительно).
const meterActive = `(m.installed_at <= CURRENT_DATE AND (m.removed_at IS NULL OR m.removed_at >= CURRENT_DATE))`

const meterSelect = `SELECT m.id, m.building_id, m.premises_id, m.resource, m.tariffs, m.serial_number, m.name, m.model, m.comment, m.coefficient,
	m.installed_at, m.removed_at, m.verified_at, m.next_verification_at, m.initial_t1, m.initial_t2, m.initial_t3,
	` + meterActive + ` AS active,
	COALESCE(m.next_verification_at < CURRENT_DATE AND (m.removed_at IS NULL OR m.removed_at >= CURRENT_DATE), false) AS verification_overdue,
	b.id AS location_building_id, b.address AS building_address, b.name AS building_name, p.kind AS premises_kind, p.number AS premises_number,
	lr.reading_date AS last_reading_date, lr.t1 AS last_t1, lr.t2 AS last_t2, lr.t3 AS last_t3,
	m.created_at, m.updated_at, m.deleted_at
	FROM meters m
	LEFT JOIN premises p ON p.id = m.premises_id
	JOIN buildings b ON b.id = COALESCE(m.building_id, p.building_id)
	LEFT JOIN LATERAL (
		SELECT reading_date, t1, t2, t3 FROM meter_readings r
		WHERE r.meter_id = m.id AND r.deleted_at IS NULL ORDER BY r.reading_date DESC LIMIT 1
	) lr ON true`

type Meters struct{ pool *pgxpool.Pool }

func NewMeters(pool *pgxpool.Pool) *Meters { return &Meters{pool} }

func (s *Meters) List(ctx context.Context, f model.MeterFilter, limit, offset int) ([]model.Meter, error) {
	return collect[model.Meter](s.pool.Query(ctx,
		meterSelect+`
		 WHERE ($1::text IS NULL OR ($1 = 'common') = (m.building_id IS NOT NULL))
		   AND ($2::bigint IS NULL OR b.id = $2)
		   AND ($3::bigint IS NULL OR m.premises_id = $3)
		   AND ($4::text IS NULL OR m.resource = $4)
		   AND ($5::bool IS NULL OR `+meterActive+` = $5)
		   AND ($6::text IS NULL OR m.serial_number ILIKE $6 OR m.name ILIKE $6 OR p.number ILIKE $7)
		   AND (m.deleted_at IS NOT NULL) = $8
		 ORDER BY m.premises_id IS NOT NULL,
		          CASE WHEN m.premises_id IS NULL THEN COALESCE(b.name, b.address) END,
		          CASE WHEN m.premises_id IS NULL THEN array_position(ARRAY['cold_water', 'hot_water', 'electricity'], m.resource) END,
		          CASE WHEN m.premises_id IS NULL THEN lower(m.name) END NULLS LAST,
		          substring(p.number from '\d+')::numeric NULLS LAST, p.number, m.resource, m.serial_number, m.id LIMIT $9 OFFSET $10`,
		f.Scope, f.BuildingID, f.PremisesID, f.Resource, f.Active, likePattern(f.Q), prefixPattern(f.Q), f.Deleted, limit, offset))
}

func (s *Meters) Get(ctx context.Context, id int64) (model.Meter, error) {
	return one[model.Meter](s.pool.Query(ctx, meterSelect+` WHERE m.id = $1`, id))
}

func (s *Meters) Create(ctx context.Context, in model.Meter) (model.Meter, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO meters (building_id, premises_id, resource, tariffs, serial_number, name, model, comment, coefficient,
		        installed_at, removed_at, verified_at, next_verification_at, initial_t1, initial_t2, initial_t3)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING id`,
		in.BuildingID, in.PremisesID, in.Resource, in.Tariffs, in.SerialNumber, in.Name, in.Model, in.Comment, in.Coefficient,
		in.InstalledAt, in.RemovedAt, in.VerifiedAt, in.NextVerificationAt, in.InitialT1, in.InitialT2, in.InitialT3).Scan(&id)
	if err != nil {
		return model.Meter{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

// Update заменяет счётчик. Параметры, от которых зависят уже введённые показания (число тарифов, даты работы,
// начальные показания), нельзя менять так, чтобы показания перестали быть корректными.
func (s *Meters) Update(ctx context.Context, id int64, in model.Meter) (model.Meter, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Meter{}, mapErr(err)
	}
	defer rollback(ctx, tx)

	var tariffs int
	if err := tx.QueryRow(ctx, `SELECT tariffs FROM meters WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&tariffs); err != nil {
		return model.Meter{}, mapErr(err)
	}
	var readings int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM meter_readings WHERE meter_id = $1 AND deleted_at IS NULL`, id).Scan(&readings); err != nil {
		return model.Meter{}, mapErr(err)
	}
	if readings > 0 {
		if tariffs != in.Tariffs {
			return model.Meter{}, invalid("tariffs: cannot be changed while the meter has readings")
		}
		var outside bool
		err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM meter_readings
			   WHERE meter_id = $1 AND deleted_at IS NULL
			     AND (reading_date < $2 OR ($3::date IS NOT NULL AND reading_date > $3)
			          OR t1 < $4 OR COALESCE(t2, 0) < $5 OR COALESCE(t3, 0) < $6))`,
			id, in.InstalledAt, in.RemovedAt, in.InitialT1, in.InitialT2, in.InitialT3).Scan(&outside)
		if err != nil {
			return model.Meter{}, mapErr(err)
		}
		if outside {
			return model.Meter{}, invalid("installed_at: existing readings are outside the working period or below the initial readings")
		}
	}
	_, err = tx.Exec(ctx,
		`UPDATE meters SET building_id = $2, premises_id = $3, resource = $4, tariffs = $5, serial_number = $6, name = $7, model = $8, comment = $9, coefficient = $10,
		        installed_at = $11, removed_at = $12, verified_at = $13, next_verification_at = $14, initial_t1 = $15, initial_t2 = $16, initial_t3 = $17, updated_at = now()
		 WHERE id = $1`,
		id, in.BuildingID, in.PremisesID, in.Resource, in.Tariffs, in.SerialNumber, in.Name, in.Model, in.Comment, in.Coefficient,
		in.InstalledAt, in.RemovedAt, in.VerifiedAt, in.NextVerificationAt, in.InitialT1, in.InitialT2, in.InitialT3)
	if err != nil {
		return model.Meter{}, mapErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Meter{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

// Restore снимает отметку удаления. Если дом, помещение или номер счётчика заняты, БД вернёт ошибку.
func (s *Meters) Restore(ctx context.Context, id int64) (model.Meter, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE meters SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	if err := checkDeleted(tag, err); err != nil {
		return model.Meter{}, err
	}
	return s.Get(ctx, id)
}

func (s *Meters) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "meters", id)
}
