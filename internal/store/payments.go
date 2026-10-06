package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

// softDelete помечает запись удалённой; триггеры БД не дадут удалить запись с действующими зависимыми.
// table — имя таблицы из кода, не из запроса.
func softDelete(ctx context.Context, pool *pgxpool.Pool, table string, id int64) error {
	tag, err := pool.Exec(ctx, `UPDATE `+table+` SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return checkDeleted(tag, err)
}

const incomingCols = `id, bank_account_id, registry_id, external_id, payment_date, payment_time, amount, commission,
	payer_name, payer_inn, payer_account, payer_bik, payer_bank_name, doc_number, operation_type, purpose, comment,
	personal_account_id, category_id, raw_line, created_at, updated_at, deleted_at,
	(SELECT a.number FROM personal_accounts a WHERE a.id = personal_account_id) AS personal_account_number,
	(SELECT r.registry_number FROM payment_registries r WHERE r.id = registry_id) AS registry_number`

type IncomingPayments struct{ pool *pgxpool.Pool }

func NewIncomingPayments(pool *pgxpool.Pool) *IncomingPayments { return &IncomingPayments{pool} }

func (s *IncomingPayments) List(ctx context.Context, f model.IncomingPaymentFilter, limit, offset int) ([]model.IncomingPayment, error) {
	return collect[model.IncomingPayment](s.pool.Query(ctx,
		`SELECT `+incomingCols+` FROM incoming_payments
		 WHERE ($1::bigint IS NULL OR bank_account_id = $1)
		   AND ($2::bigint IS NULL OR registry_id = $2)
		   AND ($3::bigint IS NULL OR personal_account_id = $3)
		   AND ($4::bigint IS NULL OR category_id = $4)
		   AND ($5::date IS NULL OR payment_date >= $5)
		   AND ($6::date IS NULL OR payment_date <= $6)
		   AND ($7::numeric IS NULL OR amount >= $7)
		   AND ($8::numeric IS NULL OR amount <= $8)
		   AND ($9::text IS NULL
		        OR concat_ws(' ', payer_name, purpose, comment, doc_number, external_id,
		                     (SELECT a.number FROM personal_accounts a WHERE a.id = personal_account_id)) ILIKE $9)
		   AND (NOT $10 OR (personal_account_id IS NULL AND category_id IS NULL))
		   AND (deleted_at IS NOT NULL) = $11
		 ORDER BY payment_date DESC, payment_time DESC NULLS LAST, id DESC LIMIT $12 OFFSET $13`,
		f.BankAccountID, f.RegistryID, f.PersonalAccountID, f.CategoryID, f.DateFrom, f.DateTo,
		f.AmountFrom, f.AmountTo, likePattern(f.Q), f.Unlinked, f.Deleted, limit, offset))
}

func (s *IncomingPayments) Get(ctx context.Context, id int64) (model.IncomingPayment, error) {
	return one[model.IncomingPayment](s.pool.Query(ctx, `SELECT `+incomingCols+` FROM incoming_payments WHERE id = $1`, id))
}

func (s *IncomingPayments) Create(ctx context.Context, in model.IncomingPayment) (model.IncomingPayment, error) {
	return one[model.IncomingPayment](s.pool.Query(ctx,
		`INSERT INTO incoming_payments (bank_account_id, registry_id, external_id, payment_date, payment_time, amount, commission,
		        payer_name, payer_inn, payer_account, payer_bik, payer_bank_name, doc_number, operation_type, purpose, comment,
		        personal_account_id, category_id, raw_line)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19) RETURNING `+incomingCols,
		in.BankAccountID, in.RegistryID, in.ExternalID, in.PaymentDate, in.PaymentTime, in.Amount, in.Commission,
		in.PayerName, in.PayerINN, in.PayerAccount, in.PayerBIK, in.PayerBankName, in.DocNumber, in.OperationType, in.Purpose, in.Comment,
		in.PersonalAccountID, in.CategoryID, in.RawLine))
}

// Update заменяет редактируемые поля. Происхождение платежа (registry_id, external_id, raw_line) не меняется.
func (s *IncomingPayments) Update(ctx context.Context, id int64, in model.IncomingPayment) (model.IncomingPayment, error) {
	return one[model.IncomingPayment](s.pool.Query(ctx,
		`UPDATE incoming_payments SET bank_account_id = $2, payment_date = $3, payment_time = $4, amount = $5, commission = $6,
		        payer_name = $7, payer_inn = $8, payer_account = $9, payer_bik = $10, payer_bank_name = $11, doc_number = $12,
		        operation_type = $13, purpose = $14, comment = $15, personal_account_id = $16, category_id = $17, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING `+incomingCols,
		id, in.BankAccountID, in.PaymentDate, in.PaymentTime, in.Amount, in.Commission,
		in.PayerName, in.PayerINN, in.PayerAccount, in.PayerBIK, in.PayerBankName, in.DocNumber,
		in.OperationType, in.Purpose, in.Comment, in.PersonalAccountID, in.CategoryID))
}

func (s *IncomingPayments) Restore(ctx context.Context, id int64) (model.IncomingPayment, error) {
	return one[model.IncomingPayment](s.pool.Query(ctx,
		`UPDATE incoming_payments SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL RETURNING `+incomingCols, id))
}

func (s *IncomingPayments) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "incoming_payments", id)
}

const outgoingCols = `id, bank_account_id, payment_date, amount, recipient_name, recipient_inn, recipient_account, recipient_bik,
	recipient_bank_name, doc_number, operation_type, purpose, comment, category_id, created_at, updated_at, deleted_at`

type OutgoingPayments struct{ pool *pgxpool.Pool }

func NewOutgoingPayments(pool *pgxpool.Pool) *OutgoingPayments { return &OutgoingPayments{pool} }

func (s *OutgoingPayments) List(ctx context.Context, f model.OutgoingPaymentFilter, limit, offset int) ([]model.OutgoingPayment, error) {
	return collect[model.OutgoingPayment](s.pool.Query(ctx,
		`SELECT `+outgoingCols+` FROM outgoing_payments
		 WHERE ($1::bigint IS NULL OR bank_account_id = $1)
		   AND ($2::bigint IS NULL OR category_id = $2)
		   AND ($3::date IS NULL OR payment_date >= $3)
		   AND ($4::date IS NULL OR payment_date <= $4)
		   AND ($5::numeric IS NULL OR amount >= $5)
		   AND ($6::numeric IS NULL OR amount <= $6)
		   AND ($7::text IS NULL OR concat_ws(' ', recipient_name, purpose, comment, doc_number) ILIKE $7)
		   AND (deleted_at IS NOT NULL) = $8
		 ORDER BY payment_date DESC, id DESC LIMIT $9 OFFSET $10`,
		f.BankAccountID, f.CategoryID, f.DateFrom, f.DateTo, f.AmountFrom, f.AmountTo, likePattern(f.Q), f.Deleted, limit, offset))
}

func (s *OutgoingPayments) Get(ctx context.Context, id int64) (model.OutgoingPayment, error) {
	return one[model.OutgoingPayment](s.pool.Query(ctx, `SELECT `+outgoingCols+` FROM outgoing_payments WHERE id = $1`, id))
}

func (s *OutgoingPayments) Create(ctx context.Context, in model.OutgoingPayment) (model.OutgoingPayment, error) {
	return one[model.OutgoingPayment](s.pool.Query(ctx,
		`INSERT INTO outgoing_payments (bank_account_id, payment_date, amount, recipient_name, recipient_inn, recipient_account,
		        recipient_bik, recipient_bank_name, doc_number, operation_type, purpose, comment, category_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING `+outgoingCols,
		in.BankAccountID, in.PaymentDate, in.Amount, in.RecipientName, in.RecipientINN, in.RecipientAccount,
		in.RecipientBIK, in.RecipientBankName, in.DocNumber, in.OperationType, in.Purpose, in.Comment, in.CategoryID))
}

func (s *OutgoingPayments) Update(ctx context.Context, id int64, in model.OutgoingPayment) (model.OutgoingPayment, error) {
	return one[model.OutgoingPayment](s.pool.Query(ctx,
		`UPDATE outgoing_payments SET bank_account_id = $2, payment_date = $3, amount = $4, recipient_name = $5, recipient_inn = $6,
		        recipient_account = $7, recipient_bik = $8, recipient_bank_name = $9, doc_number = $10, operation_type = $11,
		        purpose = $12, comment = $13, category_id = $14, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING `+outgoingCols,
		id, in.BankAccountID, in.PaymentDate, in.Amount, in.RecipientName, in.RecipientINN, in.RecipientAccount,
		in.RecipientBIK, in.RecipientBankName, in.DocNumber, in.OperationType, in.Purpose, in.Comment, in.CategoryID))
}

func (s *OutgoingPayments) Restore(ctx context.Context, id int64) (model.OutgoingPayment, error) {
	return one[model.OutgoingPayment](s.pool.Query(ctx,
		`UPDATE outgoing_payments SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL RETURNING `+outgoingCols, id))
}

func (s *OutgoingPayments) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "outgoing_payments", id)
}
