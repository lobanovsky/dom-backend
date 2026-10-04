package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type AccountHolders struct{ pool *pgxpool.Pool }

func NewAccountHolders(pool *pgxpool.Pool) *AccountHolders { return &AccountHolders{pool} }

func (s *AccountHolders) List(ctx context.Context, f model.AccountHolderFilter, limit, offset int) ([]model.AccountHolder, error) {
	return collect[model.AccountHolder](s.pool.Query(ctx,
		`SELECT id, account_id, person_id, legal_entity_id, valid_from, valid_to, created_at, updated_at FROM account_holders
		 WHERE ($1::bigint IS NULL OR account_id = $1)
		   AND ($2::bigint IS NULL OR person_id = $2)
		   AND ($3::bigint IS NULL OR legal_entity_id = $3)
		 ORDER BY id LIMIT $4 OFFSET $5`, f.AccountID, f.PersonID, f.LegalEntityID, limit, offset))
}

func (s *AccountHolders) Get(ctx context.Context, id int64) (model.AccountHolder, error) {
	return one[model.AccountHolder](s.pool.Query(ctx,
		`SELECT id, account_id, person_id, legal_entity_id, valid_from, valid_to, created_at, updated_at FROM account_holders WHERE id = $1`, id))
}

func (s *AccountHolders) Create(ctx context.Context, in model.AccountHolder) (model.AccountHolder, error) {
	return one[model.AccountHolder](s.pool.Query(ctx,
		`INSERT INTO account_holders (account_id, person_id, legal_entity_id, valid_from, valid_to) VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, account_id, person_id, legal_entity_id, valid_from, valid_to, created_at, updated_at`, in.AccountID, in.PersonID, in.LegalEntityID, in.ValidFrom, in.ValidTo))
}

func (s *AccountHolders) Update(ctx context.Context, id int64, in model.AccountHolder) (model.AccountHolder, error) {
	return one[model.AccountHolder](s.pool.Query(ctx,
		`UPDATE account_holders SET account_id = $2, person_id = $3, legal_entity_id = $4, valid_from = $5, valid_to = $6, updated_at = now()
		 WHERE id = $1 RETURNING id, account_id, person_id, legal_entity_id, valid_from, valid_to, created_at, updated_at`, id, in.AccountID, in.PersonID, in.LegalEntityID, in.ValidFrom, in.ValidTo))
}

func (s *AccountHolders) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM account_holders WHERE id = $1`, id)
	return checkDeleted(tag, err)
}
