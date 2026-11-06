package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

const kinshipCols = `id, code, name, position, created_at, updated_at, deleted_at`

type Kinships struct{ pool *pgxpool.Pool }

func NewKinships(pool *pgxpool.Pool) *Kinships { return &Kinships{pool} }

func (s *Kinships) List(ctx context.Context, f model.KinshipFilter, limit, offset int) ([]model.Kinship, error) {
	return collect[model.Kinship](s.pool.Query(ctx,
		`SELECT `+kinshipCols+` FROM kinships WHERE (deleted_at IS NOT NULL) = $1
		 ORDER BY position, id LIMIT $2 OFFSET $3`, f.Deleted, limit, offset))
}

func (s *Kinships) Get(ctx context.Context, id int64) (model.Kinship, error) {
	return one[model.Kinship](s.pool.Query(ctx, `SELECT `+kinshipCols+` FROM kinships WHERE id = $1`, id))
}

func (s *Kinships) Create(ctx context.Context, in model.Kinship) (model.Kinship, error) {
	return one[model.Kinship](s.pool.Query(ctx,
		`INSERT INTO kinships (code, name, position) VALUES ($1, $2, $3) RETURNING `+kinshipCols, in.Code, in.Name, in.Position))
}

// Update меняет название и порядок; код — идентификатор, на который ссылаются проживающие, он неизменен.
func (s *Kinships) Update(ctx context.Context, id int64, in model.Kinship) (model.Kinship, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return model.Kinship{}, err
	}
	if cur.DeletedAt == nil && cur.Code != in.Code {
		return model.Kinship{}, invalid("code: cannot be changed")
	}
	return one[model.Kinship](s.pool.Query(ctx,
		`UPDATE kinships SET name = $2, position = $3, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING `+kinshipCols, id, in.Name, in.Position))
}

func (s *Kinships) Restore(ctx context.Context, id int64) (model.Kinship, error) {
	return one[model.Kinship](s.pool.Query(ctx,
		`UPDATE kinships SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL RETURNING `+kinshipCols, id))
}

func (s *Kinships) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "kinships", id)
}
