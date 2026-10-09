package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type PremisesStore struct{ pool *pgxpool.Pool }

func NewPremisesStore(pool *pgxpool.Pool) *PremisesStore { return &PremisesStore{pool} }

func (s *PremisesStore) List(ctx context.Context, f model.PremisesFilter, limit, offset int) ([]model.Premises, error) {
	return collect[model.Premises](s.pool.Query(ctx,
		`SELECT id, building_id, kind, number, entrance, floor, total_area, living_area, rooms, cadastral_number, created_at, updated_at, deleted_at FROM premises
		 WHERE ($1::bigint IS NULL OR building_id = $1)
		   AND ($2::text IS NULL OR kind = $2)
		   AND ($3::text IS NULL OR number = $3)
		   AND ($4::text IS NULL OR number ILIKE $4)
		   AND (deleted_at IS NOT NULL) = $5
		 ORDER BY (number ILIKE $8) DESC NULLS LAST, id LIMIT $6 OFFSET $7`, // при поиске по q точное совпадение номера идёт первым
		f.BuildingID, f.Kind, f.Number, prefixPattern(f.Q), f.Deleted, limit, offset, exactPattern(f.Q)))
}

func (s *PremisesStore) Get(ctx context.Context, id int64) (model.Premises, error) {
	return one[model.Premises](s.pool.Query(ctx,
		`SELECT id, building_id, kind, number, entrance, floor, total_area, living_area, rooms, cadastral_number, created_at, updated_at, deleted_at FROM premises WHERE id = $1`, id))
}

func (s *PremisesStore) Create(ctx context.Context, in model.Premises) (model.Premises, error) {
	return one[model.Premises](s.pool.Query(ctx,
		`INSERT INTO premises (building_id, kind, number, entrance, floor, total_area, living_area, rooms, cadastral_number) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, building_id, kind, number, entrance, floor, total_area, living_area, rooms, cadastral_number, created_at, updated_at, deleted_at`, in.BuildingID, in.Kind, in.Number, in.Entrance, in.Floor, in.TotalArea, in.LivingArea, in.Rooms, in.CadastralNumber))
}

func (s *PremisesStore) Update(ctx context.Context, id int64, in model.Premises) (model.Premises, error) {
	return one[model.Premises](s.pool.Query(ctx,
		`UPDATE premises SET building_id = $2, kind = $3, number = $4, entrance = $5, floor = $6, total_area = $7, living_area = $8, rooms = $9, cadastral_number = $10, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING id, building_id, kind, number, entrance, floor, total_area, living_area, rooms, cadastral_number, created_at, updated_at, deleted_at`, id, in.BuildingID, in.Kind, in.Number, in.Entrance, in.Floor, in.TotalArea, in.LivingArea, in.Rooms, in.CadastralNumber))
}

// Restore снимает отметку удаления. Если родительская запись удалена или номер уже
// занят, БД вернёт ошибку (триггеры целостности и уникальные индексы).
func (s *PremisesStore) Restore(ctx context.Context, id int64) (model.Premises, error) {
	return one[model.Premises](s.pool.Query(ctx,
		`UPDATE premises SET deleted_at = NULL, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NOT NULL RETURNING id, building_id, kind, number, entrance, floor, total_area, living_area, rooms, cadastral_number, created_at, updated_at, deleted_at`, id))
}

func (s *PremisesStore) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE premises SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return checkDeleted(tag, err)
}
