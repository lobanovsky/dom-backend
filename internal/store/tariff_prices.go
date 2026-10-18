package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

const tariffPriceSelect = `SELECT id, tariff_id, price, valid_from, valid_to, comment, created_at, updated_at, deleted_at FROM tariff_prices`

type TariffPrices struct{ pool *pgxpool.Pool }

func NewTariffPrices(pool *pgxpool.Pool) *TariffPrices { return &TariffPrices{pool} }

func (s *TariffPrices) List(ctx context.Context, f model.TariffPriceFilter, limit, offset int) ([]model.TariffPrice, error) {
	return collect[model.TariffPrice](s.pool.Query(ctx,
		tariffPriceSelect+`
		 WHERE ($1::bigint IS NULL OR tariff_id = $1) AND (deleted_at IS NOT NULL) = $2
		 ORDER BY valid_from DESC, id DESC LIMIT $3 OFFSET $4`,
		f.TariffID, f.Deleted, limit, offset))
}

func (s *TariffPrices) Get(ctx context.Context, id int64) (model.TariffPrice, error) {
	return one[model.TariffPrice](s.pool.Query(ctx, tariffPriceSelect+` WHERE id = $1`, id))
}

func (s *TariffPrices) Create(ctx context.Context, in model.TariffPrice) (model.TariffPrice, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO tariff_prices (tariff_id, price, valid_from, valid_to, comment) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		in.TariffID, in.Price, in.ValidFrom, in.ValidTo, in.Comment).Scan(&id)
	if err != nil {
		return model.TariffPrice{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

func (s *TariffPrices) Update(ctx context.Context, id int64, in model.TariffPrice) (model.TariffPrice, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE tariff_prices SET tariff_id = $2, price = $3, valid_from = $4, valid_to = $5, comment = $6, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`,
		id, in.TariffID, in.Price, in.ValidFrom, in.ValidTo, in.Comment)
	if err := checkDeleted(tag, err); err != nil {
		return model.TariffPrice{}, err
	}
	return s.Get(ctx, id)
}

// Restore возвращает удалённую цену; если её период пересекается с действующим, БД вернёт конфликт.
func (s *TariffPrices) Restore(ctx context.Context, id int64) (model.TariffPrice, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE tariff_prices SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	if err := checkDeleted(tag, err); err != nil {
		return model.TariffPrice{}, err
	}
	return s.Get(ctx, id)
}

func (s *TariffPrices) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "tariff_prices", id)
}
