package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

const tariffSelect = `SELECT id, service_id, price, valid_from, valid_to, comment, created_at, updated_at, deleted_at FROM service_tariffs`

type ServiceTariffs struct{ pool *pgxpool.Pool }

func NewServiceTariffs(pool *pgxpool.Pool) *ServiceTariffs { return &ServiceTariffs{pool} }

func (s *ServiceTariffs) List(ctx context.Context, f model.ServiceTariffFilter, limit, offset int) ([]model.ServiceTariff, error) {
	return collect[model.ServiceTariff](s.pool.Query(ctx,
		tariffSelect+`
		 WHERE ($1::bigint IS NULL OR service_id = $1) AND (deleted_at IS NOT NULL) = $2
		 ORDER BY valid_from DESC, id DESC LIMIT $3 OFFSET $4`,
		f.ServiceID, f.Deleted, limit, offset))
}

func (s *ServiceTariffs) Get(ctx context.Context, id int64) (model.ServiceTariff, error) {
	return one[model.ServiceTariff](s.pool.Query(ctx, tariffSelect+` WHERE id = $1`, id))
}

func (s *ServiceTariffs) Create(ctx context.Context, in model.ServiceTariff) (model.ServiceTariff, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.ServiceTariff{}, mapErr(err)
	}
	defer rollback(ctx, tx)
	if err := checkTariffService(ctx, tx, in.ServiceID); err != nil {
		return model.ServiceTariff{}, err
	}
	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO service_tariffs (service_id, price, valid_from, valid_to, comment) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		in.ServiceID, in.Price, in.ValidFrom, in.ValidTo, in.Comment).Scan(&id)
	if err != nil {
		return model.ServiceTariff{}, mapErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.ServiceTariff{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

func (s *ServiceTariffs) Update(ctx context.Context, id int64, in model.ServiceTariff) (model.ServiceTariff, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.ServiceTariff{}, mapErr(err)
	}
	defer rollback(ctx, tx)
	if err := checkTariffService(ctx, tx, in.ServiceID); err != nil {
		return model.ServiceTariff{}, err
	}
	tag, err := tx.Exec(ctx,
		`UPDATE service_tariffs SET service_id = $2, price = $3, valid_from = $4, valid_to = $5, comment = $6, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`,
		id, in.ServiceID, in.Price, in.ValidFrom, in.ValidTo, in.Comment)
	if err := checkDeleted(tag, err); err != nil {
		return model.ServiceTariff{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.ServiceTariff{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

// Restore возвращает удалённый тариф; если его период пересекается с действующим, БД вернёт конфликт.
func (s *ServiceTariffs) Restore(ctx context.Context, id int64) (model.ServiceTariff, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE service_tariffs SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	if err := checkDeleted(tag, err); err != nil {
		return model.ServiceTariff{}, err
	}
	return s.Get(ctx, id)
}

func (s *ServiceTariffs) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "service_tariffs", id)
}

// checkTariffService блокирует услугу до конца транзакции и не даёт завести тариф у составной услуги.
func checkTariffService(ctx context.Context, tx pgx.Tx, serviceID int64) error {
	var method string
	err := tx.QueryRow(ctx, `SELECT calc_method FROM services WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, serviceID).Scan(&method)
	if err != nil {
		if err == pgx.ErrNoRows {
			return invalid("service_id: service not found")
		}
		return mapErr(err)
	}
	if method == "sum_prices" {
		return invalid("service_id: a sum-of-prices service has no tariffs of its own")
	}
	return nil
}
