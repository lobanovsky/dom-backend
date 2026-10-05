package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type Accounts struct{ pool *pgxpool.Pool }

func NewAccounts(pool *pgxpool.Pool) *Accounts { return &Accounts{pool} }

func (s *Accounts) List(ctx context.Context, f model.AccountFilter, limit, offset int) ([]model.Account, error) {
	return collect[model.Account](s.pool.Query(ctx,
		`SELECT id, number, premises_id, purpose, status, opened_at, closed_at, created_at, updated_at, deleted_at FROM personal_accounts
		 WHERE ($1::bigint IS NULL OR premises_id = $1)
		   AND ($2::text IS NULL OR number = $2)
		   AND ($3::text IS NULL OR status = $3)
		   AND ($4::text IS NULL OR purpose = $4)
		   AND ($5::text IS NULL OR number LIKE $5)
		   AND (deleted_at IS NOT NULL) = $6
		 ORDER BY id LIMIT $7 OFFSET $8`, f.PremisesID, f.Number, f.Status, f.Purpose, prefixPattern(f.Q), f.Deleted, limit, offset))
}

func (s *Accounts) Get(ctx context.Context, id int64) (model.Account, error) {
	return one[model.Account](s.pool.Query(ctx,
		`SELECT id, number, premises_id, purpose, status, opened_at, closed_at, created_at, updated_at, deleted_at FROM personal_accounts WHERE id = $1`, id))
}

func (s *Accounts) Create(ctx context.Context, in model.Account) (model.Account, error) {
	return one[model.Account](s.pool.Query(ctx,
		`INSERT INTO personal_accounts (number, premises_id, purpose, status, opened_at, closed_at) VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, number, premises_id, purpose, status, opened_at, closed_at, created_at, updated_at, deleted_at`, in.Number, in.PremisesID, in.Purpose, in.Status, in.OpenedAt, in.ClosedAt))
}

func (s *Accounts) Update(ctx context.Context, id int64, in model.Account) (model.Account, error) {
	return one[model.Account](s.pool.Query(ctx,
		`UPDATE personal_accounts SET number = $2, premises_id = $3, purpose = $4, status = $5, opened_at = $6, closed_at = $7, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING id, number, premises_id, purpose, status, opened_at, closed_at, created_at, updated_at, deleted_at`, id, in.Number, in.PremisesID, in.Purpose, in.Status, in.OpenedAt, in.ClosedAt))
}

// Restore снимает отметку удаления. Если родительская запись удалена или номер уже
// занят, БД вернёт ошибку (триггеры целостности и уникальные индексы).
func (s *Accounts) Restore(ctx context.Context, id int64) (model.Account, error) {
	return one[model.Account](s.pool.Query(ctx,
		`UPDATE personal_accounts SET deleted_at = NULL, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NOT NULL RETURNING id, number, premises_id, purpose, status, opened_at, closed_at, created_at, updated_at, deleted_at`, id))
}

func (s *Accounts) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE personal_accounts SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return checkDeleted(tag, err)
}

// ListByPremises возвращает счета помещения с названиями всех текущих плательщиков.
func (s *Accounts) ListByPremises(ctx context.Context, premisesID int64, deleted bool) ([]model.AccountView, error) {
	return collect[model.AccountView](s.pool.Query(ctx,
		`SELECT a.id, a.number, a.premises_id, a.purpose, a.status, a.opened_at, a.closed_at, a.created_at, a.updated_at, a.deleted_at,
		        ARRAY(SELECT COALESCE(concat_ws(' ', p.last_name, p.first_name, p.middle_name), le.name)
		              FROM account_holders h
		              LEFT JOIN persons p ON p.id = h.person_id
		              LEFT JOIN legal_entities le ON le.id = h.legal_entity_id
		              WHERE h.account_id = a.id AND h.deleted_at IS NULL AND h.valid_from <= CURRENT_DATE
		                AND (h.valid_to IS NULL OR h.valid_to >= CURRENT_DATE)
		              ORDER BY h.valid_from, h.id) AS holder_names
		 FROM personal_accounts a
		 WHERE a.premises_id = $1 AND (a.deleted_at IS NOT NULL) = $2
		 ORDER BY a.id`, premisesID, deleted))
}
