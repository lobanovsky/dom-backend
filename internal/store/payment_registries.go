package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

// RegistryExistsError — файл с таким содержимым (sha256) уже загружен.
type RegistryExistsError struct{ RegistryID int64 }

func (e *RegistryExistsError) Error() string {
	return fmt.Sprintf("registry file already loaded: registry %d", e.RegistryID)
}
func (e *RegistryExistsError) Unwrap() error { return ErrConflict }

// AllDuplicatesError — все платежи файла уже есть в базе; реестр не создан.
type AllDuplicatesError struct {
	Total   int
	Skipped []model.SkippedPayment
}

func (e *AllDuplicatesError) Error() string {
	return fmt.Sprintf("all %d payments of the file are already loaded", e.Total)
}
func (e *AllDuplicatesError) Unwrap() error { return ErrConflict }

const registryCols = `id, bank_account_id, source, file_name, file_sha256, registry_number, registry_date, payments_count,
	total_amount, total_transferred, total_commission, created_at, updated_at, deleted_at`

type PaymentRegistries struct{ pool *pgxpool.Pool }

func NewPaymentRegistries(pool *pgxpool.Pool) *PaymentRegistries { return &PaymentRegistries{pool} }

func (s *PaymentRegistries) List(ctx context.Context, f model.PaymentRegistryFilter, limit, offset int) ([]model.PaymentRegistry, error) {
	return collect[model.PaymentRegistry](s.pool.Query(ctx,
		`SELECT `+registryCols+` FROM payment_registries
		 WHERE ($1::bigint IS NULL OR bank_account_id = $1)
		   AND ($2::date IS NULL OR registry_date >= $2)
		   AND ($3::date IS NULL OR registry_date <= $3)
		   AND ($4::text IS NULL OR concat_ws(' ', file_name, registry_number) ILIKE $4)
		   AND (deleted_at IS NOT NULL) = $5
		 ORDER BY registry_date DESC NULLS LAST, id DESC LIMIT $6 OFFSET $7`,
		f.BankAccountID, f.DateFrom, f.DateTo, likePattern(f.Q), f.Deleted, limit, offset))
}

func (s *PaymentRegistries) Get(ctx context.Context, id int64) (model.PaymentRegistry, error) {
	return one[model.PaymentRegistry](s.pool.Query(ctx, `SELECT `+registryCols+` FROM payment_registries WHERE id = $1`, id))
}

// File возвращает имя и содержимое загруженного файла (как был загружен).
func (s *PaymentRegistries) File(ctx context.Context, id int64) (string, []byte, error) {
	var name string
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT file_name, file_content FROM payment_registries WHERE id = $1`, id).Scan(&name, &data)
	return name, data, mapErr(err)
}

// Import сохраняет разобранный реестр и его платежи одной транзакцией.
//
// Защита от дублей двух уровней: файл с тем же sha256 отклоняется целиком (409); платёж с уже известным
// номером операции на этом счёте (в том числе удалённый) пропускается и попадает в отчёт.
// Если пропущены все платежи, реестр не создаётся (409).
// Лицевой счёт определяется точным совпадением номера; не найден — платёж загружается без привязки.
func (s *PaymentRegistries) Import(ctx context.Context, bankAccountID int64, fileName string, data []byte, reg *model.ParsedRegistry) (model.RegistryImportResult, error) {
	res := model.RegistryImportResult{Skipped: []model.SkippedPayment{}, Warnings: []string{}}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer rollback(ctx, tx)

	var number string
	var special bool
	err = tx.QueryRow(ctx, `SELECT number, is_special FROM bank_accounts WHERE id = $1 AND deleted_at IS NULL`, bankAccountID).Scan(&number, &special)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, &Error{ErrNotFound, "bank account not found"}
	}
	if err != nil {
		return res, mapErr(err)
	}
	if reg.FileAccount != "" && reg.FileAccount != number {
		return res, invalid(fmt.Sprintf("file name refers to account %s, but account %s is selected", reg.FileAccount, number))
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	var existing int64
	switch err := tx.QueryRow(ctx, `SELECT id FROM payment_registries WHERE file_sha256 = $1`, hash).Scan(&existing); {
	case err == nil:
		return res, &RegistryExistsError{RegistryID: existing}
	case err != pgx.ErrNoRows:
		return res, mapErr(err)
	}

	known, err := knownExternalIDs(ctx, tx, bankAccountID, reg.Payments)
	if err != nil {
		return res, err
	}
	fresh := reg.Payments[:0:0]
	for _, p := range reg.Payments {
		if known[p.ExternalID] {
			res.Skipped = append(res.Skipped, model.SkippedPayment{ExternalID: p.ExternalID, PaymentDate: p.Date, PayerName: p.PayerName, Amount: float64(p.Amount) / 100})
			continue
		}
		fresh = append(fresh, p)
	}
	res.SkippedDuplicates = len(res.Skipped)
	if len(fresh) == 0 {
		return res, &AllDuplicatesError{Total: len(reg.Payments), Skipped: res.Skipped}
	}

	accounts, err := accountsByNumber(ctx, tx, fresh)
	if err != nil {
		return res, err
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO payment_registries (bank_account_id, source, file_name, file_sha256, file_content, registry_number, registry_date,
		        payments_count, total_amount, total_transferred, total_commission)
		 VALUES ($1, 'sber', $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		bankAccountID, fileName, hash, data, nilIfEmpty(reg.RegistryNumber), reg.RegistryDate, len(reg.Payments),
		float64(reg.TotalAmount)/100, float64(reg.TotalTransferred)/100, float64(reg.TotalCommission)/100).Scan(&res.RegistryID)
	if err != nil {
		return res, mapErr(err)
	}

	// Оплата по лицевому счёту «не того» типа (капремонт на обычный счёт и наоборот) загружается как есть,
	// привязка не меняется; в комментарий платежа пишется пометка, а в отчёт попадает одно предупреждение на тип.
	mismatches := map[string]int{}
	for _, p := range fresh {
		var accountID *int64
		var comment *string
		if a, ok := accounts[p.AccountNum]; ok {
			accountID = &a.id
			res.Linked++
			if note := accountTypeMismatch(a.purpose, special); note != "" {
				comment = &note
				mismatches[note]++
			}
		} else {
			res.Unlinked++
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO incoming_payments (bank_account_id, registry_id, external_id, payment_date, payment_time, amount, commission,
			        payer_name, personal_account_id, comment, raw_line)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			bankAccountID, res.RegistryID, p.ExternalID, p.Date, p.Time, float64(p.Amount)/100, float64(p.Commission)/100,
			p.PayerName, accountID, comment, p.Raw); err != nil {
			return model.RegistryImportResult{}, rowErr(p.Line, err)
		}
		res.Created++
	}
	for note, n := range mismatches {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d: %s (платежи загружены как есть, пометка добавлена в комментарий)", n, note))
	}
	sort.Strings(res.Warnings)
	if err := tx.Commit(ctx); err != nil {
		return model.RegistryImportResult{}, mapErr(err)
	}
	return res, nil
}

// accountTypeMismatch возвращает пометку, если тип лицевого счёта не соответствует типу банковского счёта,
// на который пришла оплата: капремонт на обычный счёт или остальное на специальный счёт капремонта.
func accountTypeMismatch(purpose string, specialBank bool) string {
	switch {
	case purpose == "capital_repair" && !specialBank:
		return "оплата капремонта поступила на обычный (не специальный) банковский счёт"
	case purpose != "capital_repair" && specialBank:
		return "оплата по лицевому счёту не за капремонт поступила на специальный счёт капремонта"
	}
	return ""
}

func knownExternalIDs(ctx context.Context, tx pgx.Tx, bankAccountID int64, payments []model.RegistryPayment) (map[string]bool, error) {
	ids := make([]string, len(payments))
	for i, p := range payments {
		ids[i] = p.ExternalID
	}
	rows, err := tx.Query(ctx, `SELECT external_id FROM incoming_payments WHERE bank_account_id = $1 AND external_id = ANY($2)`, bankAccountID, ids)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	known := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, mapErr(err)
		}
		known[id] = true
	}
	return known, mapErr(rows.Err())
}

type accountRef struct {
	id      int64
	purpose string
}

func accountsByNumber(ctx context.Context, tx pgx.Tx, payments []model.RegistryPayment) (map[string]accountRef, error) {
	numbers := make([]string, len(payments))
	for i, p := range payments {
		numbers[i] = p.AccountNum
	}
	rows, err := tx.Query(ctx, `SELECT number, id, purpose FROM personal_accounts WHERE deleted_at IS NULL AND number = ANY($1)`, numbers)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := map[string]accountRef{}
	for rows.Next() {
		var number string
		var a accountRef
		if err := rows.Scan(&number, &a.id, &a.purpose); err != nil {
			return nil, mapErr(err)
		}
		out[number] = a
	}
	return out, mapErr(rows.Err())
}

// BankAccountsByNumber возвращает неудалённые банковские счета: номер → id (в том числе закрытые по периоду:
// реестры за прошлые периоды тоже нужно загружать).
func (s *PaymentRegistries) BankAccountsByNumber(ctx context.Context) (map[string]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT number, id FROM bank_accounts WHERE deleted_at IS NULL`)
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
