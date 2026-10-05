package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

const paymentCategoryCols = `id, name, direction, created_at, updated_at, deleted_at`

type PaymentCategories struct{ pool *pgxpool.Pool }

func NewPaymentCategories(pool *pgxpool.Pool) *PaymentCategories { return &PaymentCategories{pool} }

func (s *PaymentCategories) List(ctx context.Context, f model.PaymentCategoryFilter, limit, offset int) ([]model.PaymentCategory, error) {
	return collect[model.PaymentCategory](s.pool.Query(ctx,
		`SELECT `+paymentCategoryCols+` FROM payment_categories
		 WHERE ($1::text IS NULL OR direction = $1) AND (deleted_at IS NOT NULL) = $2
		 ORDER BY direction, name LIMIT $3 OFFSET $4`, f.Direction, f.Deleted, limit, offset))
}

func (s *PaymentCategories) Get(ctx context.Context, id int64) (model.PaymentCategory, error) {
	return one[model.PaymentCategory](s.pool.Query(ctx, `SELECT `+paymentCategoryCols+` FROM payment_categories WHERE id = $1`, id))
}

func (s *PaymentCategories) Create(ctx context.Context, in model.PaymentCategory) (model.PaymentCategory, error) {
	return one[model.PaymentCategory](s.pool.Query(ctx,
		`INSERT INTO payment_categories (name, direction) VALUES ($1, $2) RETURNING `+paymentCategoryCols, in.Name, in.Direction))
}

func (s *PaymentCategories) Update(ctx context.Context, id int64, in model.PaymentCategory) (model.PaymentCategory, error) {
	return one[model.PaymentCategory](s.pool.Query(ctx,
		`UPDATE payment_categories SET name = $2, direction = $3, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING `+paymentCategoryCols, id, in.Name, in.Direction))
}

func (s *PaymentCategories) Restore(ctx context.Context, id int64) (model.PaymentCategory, error) {
	return one[model.PaymentCategory](s.pool.Query(ctx,
		`UPDATE payment_categories SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL RETURNING `+paymentCategoryCols, id))
}

func (s *PaymentCategories) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "payment_categories", id)
}
