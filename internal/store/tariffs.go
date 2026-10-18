package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

const tariffCatalogSelect = `SELECT t.id, t.organization_id, t.name, t.unit, t.comment, o.name AS organization_name,
	(SELECT p.price FROM tariff_prices p WHERE p.tariff_id = t.id AND p.deleted_at IS NULL AND p.valid_from <= CURRENT_DATE
	   AND (p.valid_to IS NULL OR p.valid_to >= CURRENT_DATE)) AS current_price,
	(SELECT count(*) FROM services s WHERE s.tariff_id = t.id AND s.deleted_at IS NULL)::int AS services_count,
	t.created_at, t.updated_at, t.deleted_at
	FROM tariffs t JOIN organizations o ON o.id = t.organization_id`

// Tariffs — справочник тарифов; цены тарифа — в TariffPrices.
type Tariffs struct{ pool *pgxpool.Pool }

func NewTariffs(pool *pgxpool.Pool) *Tariffs { return &Tariffs{pool} }

func (s *Tariffs) List(ctx context.Context, f model.TariffFilter, limit, offset int) ([]model.Tariff, error) {
	return collect[model.Tariff](s.pool.Query(ctx,
		tariffCatalogSelect+`
		 WHERE ($1::bigint IS NULL OR t.organization_id = $1) AND ($2::text IS NULL OR t.unit = $2) AND ($3::text IS NULL OR t.name ILIKE $3)
		   AND (t.deleted_at IS NOT NULL) = $4
		 ORDER BY t.name, t.id LIMIT $5 OFFSET $6`,
		f.OrganizationID, f.Unit, likePattern(f.Q), f.Deleted, limit, offset))
}

func (s *Tariffs) Get(ctx context.Context, id int64) (model.Tariff, error) {
	return one[model.Tariff](s.pool.Query(ctx, tariffCatalogSelect+` WHERE t.id = $1`, id))
}

func (s *Tariffs) Create(ctx context.Context, in model.Tariff) (model.Tariff, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO tariffs (organization_id, name, unit, comment) VALUES ($1, $2, $3, $4) RETURNING id`,
		in.OrganizationID, in.Name, in.Unit, in.Comment).Scan(&id)
	if err != nil {
		return model.Tariff{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

// Update: что организацию и единицу нельзя менять, пока тариф используют услуги, проверяет триггер БД.
func (s *Tariffs) Update(ctx context.Context, id int64, in model.Tariff) (model.Tariff, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE tariffs SET organization_id = $2, name = $3, unit = $4, comment = $5, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		id, in.OrganizationID, in.Name, in.Unit, in.Comment)
	if err := checkDeleted(tag, err); err != nil {
		return model.Tariff{}, err
	}
	return s.Get(ctx, id)
}

// Restore снимает отметку удаления; если название уже занято, БД вернёт ошибку.
func (s *Tariffs) Restore(ctx context.Context, id int64) (model.Tariff, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE tariffs SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	if err := checkDeleted(tag, err); err != nil {
		return model.Tariff{}, err
	}
	return s.Get(ctx, id)
}

func (s *Tariffs) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "tariffs", id)
}

// FromService выносит собственные цены услуги в новый тариф справочника в одной транзакции: цены копируются
// в тариф, у услуги удаляются (мягко), услуга начинает ссылаться на тариф.
func (s *Tariffs) FromService(ctx context.Context, req model.TariffFromServiceRequest) (model.Tariff, error) {
	if err := req.Validate(); err != nil {
		return model.Tariff{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Tariff{}, mapErr(err)
	}
	defer rollback(ctx, tx)
	var (
		orgID  int64
		name   string
		unit   string
		method string
		linked *int64
	)
	err = tx.QueryRow(ctx, `SELECT organization_id, name, unit, calc_method, tariff_id FROM services WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, req.ServiceID).
		Scan(&orgID, &name, &unit, &method, &linked)
	if err != nil {
		if err == pgx.ErrNoRows {
			return model.Tariff{}, invalid("service_id: service not found")
		}
		return model.Tariff{}, mapErr(err)
	}
	switch {
	case method == "sum_prices":
		return model.Tariff{}, invalid("service_id: a sum-of-prices service has no tariffs of its own")
	case linked != nil:
		return model.Tariff{}, invalid("service_id: service already uses a catalog tariff")
	}
	var has bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM service_tariffs WHERE service_id = $1 AND deleted_at IS NULL)`, req.ServiceID).Scan(&has); err != nil {
		return model.Tariff{}, mapErr(err)
	}
	if !has {
		return model.Tariff{}, invalid("service_id: service has no tariffs of its own")
	}
	if req.Name != nil && *req.Name != "" {
		name = *req.Name
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO tariffs (organization_id, name, unit) VALUES ($1, $2, $3) RETURNING id`, orgID, name, unit).Scan(&id); err != nil {
		return model.Tariff{}, mapErr(err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO tariff_prices (tariff_id, price, valid_from, valid_to, comment)
		 SELECT $1, price, valid_from, valid_to, comment FROM service_tariffs WHERE service_id = $2 AND deleted_at IS NULL`, id, req.ServiceID); err != nil {
		return model.Tariff{}, mapErr(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE service_tariffs SET deleted_at = now(), updated_at = now() WHERE service_id = $1 AND deleted_at IS NULL`, req.ServiceID); err != nil {
		return model.Tariff{}, mapErr(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE services SET tariff_id = $2, updated_at = now() WHERE id = $1`, req.ServiceID, id); err != nil {
		return model.Tariff{}, mapErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Tariff{}, mapErr(err)
	}
	return s.Get(ctx, id)
}
