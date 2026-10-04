package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type Organizations struct{ pool *pgxpool.Pool }

func NewOrganizations(pool *pgxpool.Pool) *Organizations { return &Organizations{pool} }

func (s *Organizations) List(ctx context.Context, f model.OrganizationFilter, limit, offset int) ([]model.Organization, error) {
	return collect[model.Organization](s.pool.Query(ctx,
		`SELECT id, kind, name, inn, kpp, ogrn, created_at, updated_at, deleted_at FROM organizations
		 WHERE ($1::text IS NULL OR kind = $1)
		   AND (deleted_at IS NOT NULL) = $2
		 ORDER BY id LIMIT $3 OFFSET $4`, f.Kind, f.Deleted, limit, offset))
}

func (s *Organizations) Get(ctx context.Context, id int64) (model.Organization, error) {
	return one[model.Organization](s.pool.Query(ctx,
		`SELECT id, kind, name, inn, kpp, ogrn, created_at, updated_at, deleted_at FROM organizations WHERE id = $1`, id))
}

func (s *Organizations) Create(ctx context.Context, in model.Organization) (model.Organization, error) {
	return one[model.Organization](s.pool.Query(ctx,
		`INSERT INTO organizations (kind, name, inn, kpp, ogrn) VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, kind, name, inn, kpp, ogrn, created_at, updated_at, deleted_at`, in.Kind, in.Name, in.INN, in.KPP, in.OGRN))
}

func (s *Organizations) Update(ctx context.Context, id int64, in model.Organization) (model.Organization, error) {
	return one[model.Organization](s.pool.Query(ctx,
		`UPDATE organizations SET kind = $2, name = $3, inn = $4, kpp = $5, ogrn = $6, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING id, kind, name, inn, kpp, ogrn, created_at, updated_at, deleted_at`, id, in.Kind, in.Name, in.INN, in.KPP, in.OGRN))
}

// Restore снимает отметку удаления. Если родительская запись удалена или номер уже
// занят, БД вернёт ошибку (триггеры целостности и уникальные индексы).
func (s *Organizations) Restore(ctx context.Context, id int64) (model.Organization, error) {
	return one[model.Organization](s.pool.Query(ctx,
		`UPDATE organizations SET deleted_at = NULL, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NOT NULL RETURNING id, kind, name, inn, kpp, ogrn, created_at, updated_at, deleted_at`, id))
}

func (s *Organizations) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE organizations SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return checkDeleted(tag, err)
}
