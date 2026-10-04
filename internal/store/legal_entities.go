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
		`SELECT id, name, inn, kpp, ogrn, created_at, updated_at FROM legal_entities
		 WHERE ($1::text IS NULL OR inn = $1)
		 ORDER BY id LIMIT $2 OFFSET $3`, f.INN, limit, offset))
}

func (s *LegalEntities) Get(ctx context.Context, id int64) (model.LegalEntity, error) {
	return one[model.LegalEntity](s.pool.Query(ctx,
		`SELECT id, name, inn, kpp, ogrn, created_at, updated_at FROM legal_entities WHERE id = $1`, id))
}

func (s *LegalEntities) Create(ctx context.Context, in model.LegalEntity) (model.LegalEntity, error) {
	return one[model.LegalEntity](s.pool.Query(ctx,
		`INSERT INTO legal_entities (name, inn, kpp, ogrn) VALUES ($1, $2, $3, $4)
		 RETURNING id, name, inn, kpp, ogrn, created_at, updated_at`, in.Name, in.INN, in.KPP, in.OGRN))
}

func (s *LegalEntities) Update(ctx context.Context, id int64, in model.LegalEntity) (model.LegalEntity, error) {
	return one[model.LegalEntity](s.pool.Query(ctx,
		`UPDATE legal_entities SET name = $2, inn = $3, kpp = $4, ogrn = $5, updated_at = now()
		 WHERE id = $1 RETURNING id, name, inn, kpp, ogrn, created_at, updated_at`, id, in.Name, in.INN, in.KPP, in.OGRN))
}

func (s *LegalEntities) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM legal_entities WHERE id = $1`, id)
	return checkDeleted(tag, err)
}
