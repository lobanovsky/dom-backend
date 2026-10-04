package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type LegalEntities struct{ pool *pgxpool.Pool }

func NewLegalEntities(pool *pgxpool.Pool) *LegalEntities { return &LegalEntities{pool} }

func (s *LegalEntities) List(ctx context.Context, f model.LegalEntityFilter, limit, offset int) ([]model.LegalEntity, error) {
	return collect[model.LegalEntity](s.pool.Query(ctx,
		`SELECT id, name, inn, kpp, ogrn, created_at, updated_at, deleted_at FROM legal_entities
		 WHERE ($1::text IS NULL OR inn = $1)
		   AND ($2::text IS NULL OR name ILIKE $2 OR inn ILIKE $2)
		   AND (deleted_at IS NOT NULL) = $3
		 ORDER BY name, id LIMIT $4 OFFSET $5`, f.INN, likePattern(f.Q), f.Deleted, limit, offset))
}

func (s *LegalEntities) Get(ctx context.Context, id int64) (model.LegalEntity, error) {
	return one[model.LegalEntity](s.pool.Query(ctx,
		`SELECT id, name, inn, kpp, ogrn, created_at, updated_at, deleted_at FROM legal_entities WHERE id = $1`, id))
}

func (s *LegalEntities) Create(ctx context.Context, in model.LegalEntity) (model.LegalEntity, error) {
	return one[model.LegalEntity](s.pool.Query(ctx,
		`INSERT INTO legal_entities (name, inn, kpp, ogrn) VALUES ($1, $2, $3, $4)
		 RETURNING id, name, inn, kpp, ogrn, created_at, updated_at, deleted_at`, in.Name, in.INN, in.KPP, in.OGRN))
}

func (s *LegalEntities) Update(ctx context.Context, id int64, in model.LegalEntity) (model.LegalEntity, error) {
	return one[model.LegalEntity](s.pool.Query(ctx,
		`UPDATE legal_entities SET name = $2, inn = $3, kpp = $4, ogrn = $5, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING id, name, inn, kpp, ogrn, created_at, updated_at, deleted_at`, id, in.Name, in.INN, in.KPP, in.OGRN))
}

// Restore снимает отметку удаления. Если родительская запись удалена или номер уже
// занят, БД вернёт ошибку (триггеры целостности и уникальные индексы).
func (s *LegalEntities) Restore(ctx context.Context, id int64) (model.LegalEntity, error) {
	return one[model.LegalEntity](s.pool.Query(ctx,
		`UPDATE legal_entities SET deleted_at = NULL, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NOT NULL RETURNING id, name, inn, kpp, ogrn, created_at, updated_at, deleted_at`, id))
}

func (s *LegalEntities) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE legal_entities SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return checkDeleted(tag, err)
}
