package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

const premisesKindCols = `id, code, name, position, created_at, updated_at, deleted_at`

type PremisesKinds struct{ pool *pgxpool.Pool }

func NewPremisesKinds(pool *pgxpool.Pool) *PremisesKinds { return &PremisesKinds{pool} }

func (s *PremisesKinds) List(ctx context.Context, f model.PremisesKindFilter, limit, offset int) ([]model.PremisesKind, error) {
	return collect[model.PremisesKind](s.pool.Query(ctx,
		`SELECT `+premisesKindCols+` FROM premises_kinds WHERE (deleted_at IS NOT NULL) = $1
		 ORDER BY position, id LIMIT $2 OFFSET $3`, f.Deleted, limit, offset))
}

func (s *PremisesKinds) Get(ctx context.Context, id int64) (model.PremisesKind, error) {
	return one[model.PremisesKind](s.pool.Query(ctx, `SELECT `+premisesKindCols+` FROM premises_kinds WHERE id = $1`, id))
}

func (s *PremisesKinds) Create(ctx context.Context, in model.PremisesKind) (model.PremisesKind, error) {
	return one[model.PremisesKind](s.pool.Query(ctx,
		`INSERT INTO premises_kinds (code, name, position) VALUES ($1, $2, $3) RETURNING `+premisesKindCols, in.Code, in.Name, in.Position))
}

// Update меняет название и порядок; код — идентификатор, на который ссылаются помещения, он неизменен.
func (s *PremisesKinds) Update(ctx context.Context, id int64, in model.PremisesKind) (model.PremisesKind, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return model.PremisesKind{}, err
	}
	if cur.DeletedAt == nil && cur.Code != in.Code {
		return model.PremisesKind{}, invalid("code: cannot be changed")
	}
	return one[model.PremisesKind](s.pool.Query(ctx,
		`UPDATE premises_kinds SET name = $2, position = $3, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING `+premisesKindCols, id, in.Name, in.Position))
}

func (s *PremisesKinds) Restore(ctx context.Context, id int64) (model.PremisesKind, error) {
	return one[model.PremisesKind](s.pool.Query(ctx,
		`UPDATE premises_kinds SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL RETURNING `+premisesKindCols, id))
}

func (s *PremisesKinds) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "premises_kinds", id)
}
