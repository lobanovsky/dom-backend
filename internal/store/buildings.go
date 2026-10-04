package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type Buildings struct{ pool *pgxpool.Pool }

func NewBuildings(pool *pgxpool.Pool) *Buildings { return &Buildings{pool} }

func (s *Buildings) List(ctx context.Context, f model.BuildingFilter, limit, offset int) ([]model.Building, error) {
	return collect[model.Building](s.pool.Query(ctx,
		`SELECT id, organization_id, kind, address, cadastral_number, floors, entrances, total_area, year_built, created_at, updated_at, deleted_at FROM buildings
		 WHERE ($1::bigint IS NULL OR organization_id = $1)
		   AND ($2::text IS NULL OR kind = $2)
		   AND (deleted_at IS NOT NULL) = $3
		 ORDER BY id LIMIT $4 OFFSET $5`, f.OrganizationID, f.Kind, f.Deleted, limit, offset))
}

func (s *Buildings) Get(ctx context.Context, id int64) (model.Building, error) {
	return one[model.Building](s.pool.Query(ctx,
		`SELECT id, organization_id, kind, address, cadastral_number, floors, entrances, total_area, year_built, created_at, updated_at, deleted_at FROM buildings WHERE id = $1`, id))
}

func (s *Buildings) Create(ctx context.Context, in model.Building) (model.Building, error) {
	return one[model.Building](s.pool.Query(ctx,
		`INSERT INTO buildings (organization_id, kind, address, cadastral_number, floors, entrances, total_area, year_built) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, organization_id, kind, address, cadastral_number, floors, entrances, total_area, year_built, created_at, updated_at, deleted_at`, in.OrganizationID, in.Kind, in.Address, in.CadastralNumber, in.Floors, in.Entrances, in.TotalArea, in.YearBuilt))
}

func (s *Buildings) Update(ctx context.Context, id int64, in model.Building) (model.Building, error) {
	return one[model.Building](s.pool.Query(ctx,
		`UPDATE buildings SET organization_id = $2, kind = $3, address = $4, cadastral_number = $5, floors = $6, entrances = $7, total_area = $8, year_built = $9, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING id, organization_id, kind, address, cadastral_number, floors, entrances, total_area, year_built, created_at, updated_at, deleted_at`, id, in.OrganizationID, in.Kind, in.Address, in.CadastralNumber, in.Floors, in.Entrances, in.TotalArea, in.YearBuilt))
}

// Restore снимает отметку удаления. Если родительская запись удалена или номер уже
// занят, БД вернёт ошибку (триггеры целостности и уникальные индексы).
func (s *Buildings) Restore(ctx context.Context, id int64) (model.Building, error) {
	return one[model.Building](s.pool.Query(ctx,
		`UPDATE buildings SET deleted_at = NULL, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NOT NULL RETURNING id, organization_id, kind, address, cadastral_number, floors, entrances, total_area, year_built, created_at, updated_at, deleted_at`, id))
}

func (s *Buildings) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE buildings SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return checkDeleted(tag, err)
}
