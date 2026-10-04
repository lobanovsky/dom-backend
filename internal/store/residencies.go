package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type Residencies struct{ pool *pgxpool.Pool }

func NewResidencies(pool *pgxpool.Pool) *Residencies { return &Residencies{pool} }

func (s *Residencies) List(ctx context.Context, f model.ResidencyFilter, limit, offset int) ([]model.Residency, error) {
	return collect[model.Residency](s.pool.Query(ctx,
		`SELECT id, person_id, premises_id, registered, relation, related_owner_id, valid_from, valid_to, created_at, updated_at, deleted_at FROM residencies
		 WHERE ($1::bigint IS NULL OR premises_id = $1)
		   AND ($2::bigint IS NULL OR person_id = $2)
		   AND ($3::bigint IS NULL OR related_owner_id = $3)
		   AND (deleted_at IS NOT NULL) = $4
		 ORDER BY id LIMIT $5 OFFSET $6`, f.PremisesID, f.PersonID, f.RelatedOwnerID, f.Deleted, limit, offset))
}

func (s *Residencies) Get(ctx context.Context, id int64) (model.Residency, error) {
	return one[model.Residency](s.pool.Query(ctx,
		`SELECT id, person_id, premises_id, registered, relation, related_owner_id, valid_from, valid_to, created_at, updated_at, deleted_at FROM residencies WHERE id = $1`, id))
}

func (s *Residencies) Create(ctx context.Context, in model.Residency) (model.Residency, error) {
	return one[model.Residency](s.pool.Query(ctx,
		`INSERT INTO residencies (person_id, premises_id, registered, relation, related_owner_id, valid_from, valid_to) VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, person_id, premises_id, registered, relation, related_owner_id, valid_from, valid_to, created_at, updated_at, deleted_at`, in.PersonID, in.PremisesID, in.Registered, in.Relation, in.RelatedOwnerID, in.ValidFrom, in.ValidTo))
}

func (s *Residencies) Update(ctx context.Context, id int64, in model.Residency) (model.Residency, error) {
	return one[model.Residency](s.pool.Query(ctx,
		`UPDATE residencies SET person_id = $2, premises_id = $3, registered = $4, relation = $5, related_owner_id = $6, valid_from = $7, valid_to = $8, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING id, person_id, premises_id, registered, relation, related_owner_id, valid_from, valid_to, created_at, updated_at, deleted_at`, id, in.PersonID, in.PremisesID, in.Registered, in.Relation, in.RelatedOwnerID, in.ValidFrom, in.ValidTo))
}

// Restore снимает отметку удаления. Если родительская запись удалена или номер уже
// занят, БД вернёт ошибку (триггеры целостности и уникальные индексы).
func (s *Residencies) Restore(ctx context.Context, id int64) (model.Residency, error) {
	return one[model.Residency](s.pool.Query(ctx,
		`UPDATE residencies SET deleted_at = NULL, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NOT NULL RETURNING id, person_id, premises_id, registered, relation, related_owner_id, valid_from, valid_to, created_at, updated_at, deleted_at`, id))
}

func (s *Residencies) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE residencies SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return checkDeleted(tag, err)
}
