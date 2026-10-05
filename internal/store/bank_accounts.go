package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

// Активность вычисляется по периоду действия на сегодня.
const bankAccountActive = `(valid_from <= CURRENT_DATE AND (valid_to IS NULL OR valid_to >= CURRENT_DATE))`

const bankAccountCols = `id, organization_id, number, bik, bank_name, is_special, description, valid_from, valid_to, ` +
	bankAccountActive + ` AS active, created_at, updated_at, deleted_at`

type BankAccounts struct{ pool *pgxpool.Pool }

func NewBankAccounts(pool *pgxpool.Pool) *BankAccounts { return &BankAccounts{pool} }

func (s *BankAccounts) List(ctx context.Context, f model.BankAccountFilter, limit, offset int) ([]model.BankAccount, error) {
	return collect[model.BankAccount](s.pool.Query(ctx,
		`SELECT `+bankAccountCols+` FROM bank_accounts
		 WHERE ($1::bigint IS NULL OR organization_id = $1)
		   AND ($2::bool IS NULL OR is_special = $2)
		   AND ($3::bool IS NULL OR `+bankAccountActive+` = $3)
		   AND (deleted_at IS NOT NULL) = $4
		 ORDER BY is_special, number LIMIT $5 OFFSET $6`, f.OrganizationID, f.IsSpecial, f.Active, f.Deleted, limit, offset))
}

func (s *BankAccounts) Get(ctx context.Context, id int64) (model.BankAccount, error) {
	return one[model.BankAccount](s.pool.Query(ctx, `SELECT `+bankAccountCols+` FROM bank_accounts WHERE id = $1`, id))
}

func (s *BankAccounts) Create(ctx context.Context, in model.BankAccount) (model.BankAccount, error) {
	return one[model.BankAccount](s.pool.Query(ctx,
		`INSERT INTO bank_accounts (organization_id, number, bik, bank_name, is_special, description, valid_from, valid_to)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+bankAccountCols,
		in.OrganizationID, in.Number, in.BIK, in.BankName, in.IsSpecial, in.Description, in.ValidFrom, in.ValidTo))
}

func (s *BankAccounts) Update(ctx context.Context, id int64, in model.BankAccount) (model.BankAccount, error) {
	return one[model.BankAccount](s.pool.Query(ctx,
		`UPDATE bank_accounts SET organization_id = $2, number = $3, bik = $4, bank_name = $5, is_special = $6, description = $7,
		        valid_from = $8, valid_to = $9, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING `+bankAccountCols,
		id, in.OrganizationID, in.Number, in.BIK, in.BankName, in.IsSpecial, in.Description, in.ValidFrom, in.ValidTo))
}

func (s *BankAccounts) Restore(ctx context.Context, id int64) (model.BankAccount, error) {
	return one[model.BankAccount](s.pool.Query(ctx,
		`UPDATE bank_accounts SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL RETURNING `+bankAccountCols, id))
}

func (s *BankAccounts) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "bank_accounts", id)
}
