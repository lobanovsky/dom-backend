package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

// StatementExistsError — файл с таким содержимым (sha256) уже загружен.
type StatementExistsError struct {
	StatementID int64
	FileName    string
}

func (e *StatementExistsError) Error() string {
	return fmt.Sprintf("statement file already loaded: statement %d", e.StatementID)
}
func (e *StatementExistsError) Unwrap() error { return ErrConflict }

// StatementAllDuplicatesError — все операции выписки уже есть в базе (из других выписок); выписка не создана.
type StatementAllDuplicatesError struct {
	Total   int
	Skipped []model.SkippedOperation
}

func (e *StatementAllDuplicatesError) Error() string {
	return fmt.Sprintf("all %d operations of the statement are already loaded", e.Total)
}
func (e *StatementAllDuplicatesError) Unwrap() error { return ErrConflict }

// UnknownBankAccountError — счёт выписки не заведён в системе.
type UnknownBankAccountError struct{ Number string }

func (e *UnknownBankAccountError) Error() string {
	return "bank account " + e.Number + " is not in the system"
}
func (e *UnknownBankAccountError) Unwrap() error { return ErrNotFound }

const statementCols = `id, bank_account_id, file_name, file_sha256, period_from, period_to, opening_balance, closing_balance,
	debit_count, credit_count, debit_total, credit_total, created_at, updated_at, deleted_at`

type BankStatements struct{ pool *pgxpool.Pool }

func NewBankStatements(pool *pgxpool.Pool) *BankStatements { return &BankStatements{pool} }

func (s *BankStatements) List(ctx context.Context, f model.BankStatementFilter, limit, offset int) ([]model.BankStatement, error) {
	return collect[model.BankStatement](s.pool.Query(ctx,
		`SELECT `+statementCols+` FROM bank_statements
		 WHERE ($1::bigint IS NULL OR bank_account_id = $1)
		   AND ($2::date IS NULL OR period_to >= $2)
		   AND ($3::date IS NULL OR period_from <= $3)
		   AND ($4::text IS NULL OR file_name ILIKE $4)
		   AND (deleted_at IS NOT NULL) = $5
		 ORDER BY period_to DESC NULLS LAST, id DESC LIMIT $6 OFFSET $7`,
		f.BankAccountID, f.DateFrom, f.DateTo, likePattern(f.Q), f.Deleted, limit, offset))
}

func (s *BankStatements) Get(ctx context.Context, id int64) (model.BankStatement, error) {
	return one[model.BankStatement](s.pool.Query(ctx, `SELECT `+statementCols+` FROM bank_statements WHERE id = $1`, id))
}

// File возвращает имя и содержимое загруженного файла.
func (s *BankStatements) File(ctx context.Context, id int64) (string, []byte, error) {
	var name string
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT file_name, file_content FROM bank_statements WHERE id = $1`, id).Scan(&name, &data)
	return name, data, mapErr(err)
}

// BankAccountsByNumber: номер → id неудалённых банковских счетов (в том числе закрытых по периоду).
func (s *BankStatements) BankAccountsByNumber(ctx context.Context) (map[string]int64, error) {
	return bankAccountsByNumber(ctx, s.pool)
}

func bankAccountsByNumber(ctx context.Context, pool *pgxpool.Pool) (map[string]int64, error) {
	rows, err := pool.Query(ctx, `SELECT number, id FROM bank_accounts WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var number string
		var id int64
		if err := rows.Scan(&number, &id); err != nil {
			return nil, mapErr(err)
		}
		out[number] = id
	}
	return out, mapErr(rows.Err())
}

// Import сохраняет выписку и её операции одной транзакцией. Поступления становятся входящими платежами,
// списания исходящими.
//
// Защита от дублей двух уровней: файл с тем же sha256 отклоняется целиком; операция с уже известным
// dedup_key на этом счёте (из другой, пересекающейся выписки, в том числе удалённая) пропускается и попадает в отчёт.
// Если пропущены все операции, выписка не создаётся.
func (s *BankStatements) Import(ctx context.Context, fileName string, data []byte, st *model.ParsedStatement) (model.StatementImportResult, error) {
	res := model.StatementImportResult{Skipped: []model.SkippedOperation{}, Warnings: []string{}}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer rollback(ctx, tx)

	var bankID int64
	err = tx.QueryRow(ctx, `SELECT id FROM bank_accounts WHERE number = $1 AND deleted_at IS NULL`, st.Account).Scan(&bankID)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, &UnknownBankAccountError{Number: st.Account}
	}
	if err != nil {
		return res, mapErr(err)
	}

	// Ключ файла: содержимое; у файла с несколькими выписками (по листу на счёт) ещё и лист, иначе уникальность ломала бы вторую.
	keyed := data
	if st.MultiSheet {
		keyed = append(append(append([]byte{}, data...), 0), st.Sheet...)
	}
	sum := sha256.Sum256(keyed)
	hash := hex.EncodeToString(sum[:])
	var existing int64
	var existingName string
	switch err := tx.QueryRow(ctx, `SELECT id, file_name FROM bank_statements WHERE file_sha256 = $1`, hash).Scan(&existing, &existingName); {
	case err == nil:
		return res, &StatementExistsError{StatementID: existing, FileName: existingName}
	case !errors.Is(err, pgx.ErrNoRows):
		return res, mapErr(err)
	}

	known, err := knownDedupKeys(ctx, tx, bankID, st.Operations)
	if err != nil {
		return res, err
	}
	var fresh []model.StatementOperation
	for _, op := range st.Operations {
		if known[op.DedupKey] {
			res.Skipped = append(res.Skipped, model.SkippedOperation{
				PaymentDate: model.Date{Time: day(op)}, Outgoing: op.Outgoing, Counterpart: op.CounterName, DocNumber: op.DocNumber, Amount: float64(op.Amount) / 100,
			})
			continue
		}
		fresh = append(fresh, op)
	}
	res.SkippedDuplicates = len(res.Skipped)
	if len(fresh) == 0 {
		return res, &StatementAllDuplicatesError{Total: len(st.Operations), Skipped: res.Skipped}
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO bank_statements (bank_account_id, file_name, file_sha256, file_content, period_from, period_to,
		        opening_balance, closing_balance, debit_count, credit_count, debit_total, credit_total)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id`,
		bankID, fileName, hash, data, st.PeriodFrom, st.PeriodTo, kopecksToRub(st.OpeningBalance), kopecksToRub(st.ClosingBalance),
		st.DebitCount, st.CreditCount, float64(st.DebitTotal)/100, float64(st.CreditTotal)/100).Scan(&res.StatementID)
	if err != nil {
		return res, mapErr(err)
	}

	for _, op := range fresh {
		if err := insertOperation(ctx, tx, bankID, res.StatementID, op); err != nil {
			return model.StatementImportResult{}, rowErr(op.Row, err)
		}
		if op.Outgoing {
			res.Outgoing++
		} else {
			res.Incoming++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.StatementImportResult{}, mapErr(err)
	}
	return res, nil
}

func day(op model.StatementOperation) (t time.Time) {
	return time.Date(op.At.Year(), op.At.Month(), op.At.Day(), 0, 0, 0, 0, time.UTC)
}

func kopecksToRub(v *int64) *float64 {
	if v == nil {
		return nil
	}
	f := float64(*v) / 100
	return &f
}

func insertOperation(ctx context.Context, tx pgx.Tx, bankID, statementID int64, op model.StatementOperation) error {
	clock := model.Clock(op.At.Format("15:04:05"))
	date := model.Date{Time: day(op)}
	amount := float64(op.Amount) / 100
	if op.Outgoing {
		_, err := tx.Exec(ctx,
			`INSERT INTO outgoing_payments (bank_account_id, statement_id, payment_date, amount, recipient_name, recipient_inn,
			        recipient_account, recipient_bik, recipient_bank_name, doc_number, operation_type, purpose, dedup_key)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			bankID, statementID, date, amount, op.CounterName, nilIfEmpty(op.CounterINN), nilIfEmpty(op.CounterAccount),
			nilIfEmpty(op.BIK), nilIfEmpty(op.BankName), nilIfEmpty(op.DocNumber), nilIfEmpty(op.OperationType), nilIfEmpty(op.Purpose), op.DedupKey)
		return err
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO incoming_payments (bank_account_id, statement_id, payment_date, payment_time, amount, payer_name, payer_inn,
		        payer_account, payer_bik, payer_bank_name, doc_number, operation_type, purpose, raw_line, dedup_key)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		bankID, statementID, date, clock, amount, op.CounterName, nilIfEmpty(op.CounterINN), nilIfEmpty(op.CounterAccount),
		nilIfEmpty(op.BIK), nilIfEmpty(op.BankName), nilIfEmpty(op.DocNumber), nilIfEmpty(op.OperationType), nilIfEmpty(op.Purpose), nilIfEmpty(op.Raw), op.DedupKey)
	return err
}

func knownDedupKeys(ctx context.Context, tx pgx.Tx, bankID int64, ops []model.StatementOperation) (map[string]bool, error) {
	keys := make([]string, len(ops))
	for i, op := range ops {
		keys[i] = op.DedupKey
	}
	known := map[string]bool{}
	for _, table := range []string{"incoming_payments", "outgoing_payments"} {
		rows, err := tx.Query(ctx, `SELECT dedup_key FROM `+table+` WHERE bank_account_id = $1 AND dedup_key = ANY($2)`, bankID, keys)
		if err != nil {
			return nil, mapErr(err)
		}
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				rows.Close()
				return nil, mapErr(err)
			}
			known[k] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, mapErr(err)
		}
	}
	return known, nil
}
