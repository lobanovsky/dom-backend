package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

const normSelect = `SELECT n.id, n.service_id, n.building_id, n.value, n.valid_from, n.valid_to, n.comment, b.address AS building_address, b.name AS building_name,
	n.created_at, n.updated_at, n.deleted_at
	FROM service_norms n JOIN buildings b ON b.id = n.building_id`

type ServiceNorms struct{ pool *pgxpool.Pool }

func NewServiceNorms(pool *pgxpool.Pool) *ServiceNorms { return &ServiceNorms{pool} }

func (s *ServiceNorms) List(ctx context.Context, f model.ServiceNormFilter, limit, offset int) ([]model.ServiceNorm, error) {
	return collect[model.ServiceNorm](s.pool.Query(ctx,
		normSelect+`
		 WHERE ($1::bigint IS NULL OR n.service_id = $1) AND ($2::bigint IS NULL OR n.building_id = $2) AND (n.deleted_at IS NOT NULL) = $3
		 ORDER BY b.address, n.valid_from DESC, n.id DESC LIMIT $4 OFFSET $5`,
		f.ServiceID, f.BuildingID, f.Deleted, limit, offset))
}

func (s *ServiceNorms) Get(ctx context.Context, id int64) (model.ServiceNorm, error) {
	return one[model.ServiceNorm](s.pool.Query(ctx, normSelect+` WHERE n.id = $1`, id))
}

// Create и Update: что норматив бывает только у услуги «площадь × норматив» и для дома её организации, проверяет триггер БД.
func (s *ServiceNorms) Create(ctx context.Context, in model.ServiceNorm) (model.ServiceNorm, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO service_norms (service_id, building_id, value, valid_from, valid_to, comment) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		in.ServiceID, in.BuildingID, in.Value, in.ValidFrom, in.ValidTo, in.Comment).Scan(&id)
	if err != nil {
		return model.ServiceNorm{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

func (s *ServiceNorms) Update(ctx context.Context, id int64, in model.ServiceNorm) (model.ServiceNorm, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE service_norms SET service_id = $2, building_id = $3, value = $4, valid_from = $5, valid_to = $6, comment = $7, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`,
		id, in.ServiceID, in.BuildingID, in.Value, in.ValidFrom, in.ValidTo, in.Comment)
	if err := checkDeleted(tag, err); err != nil {
		return model.ServiceNorm{}, err
	}
	return s.Get(ctx, id)
}

// Restore возвращает удалённый норматив; если его период пересекается с действующим, БД вернёт конфликт.
func (s *ServiceNorms) Restore(ctx context.Context, id int64) (model.ServiceNorm, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE service_norms SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	if err := checkDeleted(tag, err); err != nil {
		return model.ServiceNorm{}, err
	}
	return s.Get(ctx, id)
}

func (s *ServiceNorms) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "service_norms", id)
}
