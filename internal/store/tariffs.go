package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

const tariffCatalogSelect = `SELECT t.id, t.organization_id, t.name, t.unit, t.comment, o.name AS organization_name,
	cp.price AS current_price, cp.valid_from AS current_price_from, cp.valid_to AS current_price_to,
	(SELECT count(*) FROM services s WHERE s.tariff_id = t.id AND s.deleted_at IS NULL)::int AS services_count,
	t.created_at, t.updated_at, t.deleted_at
	FROM tariffs t JOIN organizations o ON o.id = t.organization_id
	LEFT JOIN LATERAL (SELECT p.price, p.valid_from, p.valid_to FROM tariff_prices p WHERE p.tariff_id = t.id AND p.deleted_at IS NULL
	   AND p.valid_from <= CURRENT_DATE AND (p.valid_to IS NULL OR p.valid_to >= CURRENT_DATE)) cp ON true`

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
