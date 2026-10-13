package store

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
	"dom-backend/internal/sberexport"
)

// maxExportRows — предел платежей в одном реестре: файл остаётся разумного размера, а загрузка в принимающую систему быстрой.
const maxExportRows = 20000

// exportLockKey — пространство ключей advisory-блокировки нумерации реестров (второй ключ — банковский счёт).
const exportLockKey = 7316

var errTooMany = invalid(fmt.Sprintf("too many payments: at most %d per registry", maxExportRows))

const exportCols = `id, bank_account_id, number, file_name, payments_count, total_amount, created_at, updated_at, deleted_at`

// ExportSelection — какие платежи выгружать. Допустимые платежи отбираются сервером: из выписки (не из реестра),
// с лицевым счётом, не удалённые и ещё не выгруженные; поверх этого действует фильтр списка.
type ExportSelection struct {
	// Mode: "ids" — выгрузить платежи из IDs; "filter" — все по фильтру, кроме IDs (исключения).
	Mode string
	IDs  []int64
	// ExpectedCount — сколько платежей пользователь видел в предпросмотре; если сейчас иначе — конфликт (список изменился).
	ExpectedCount *int
}

// ExportPreview — что попадёт в реестр.
type ExportPreview struct {
	Count          int     `json:"count"`
	Total          float64 `json:"total"`
	BankAccountIDs []int64 `json:"bank_account_ids"`
}

type exportRow struct {
	ID            int64
	BankAccountID int64
	Date          time.Time
	Time          *string
	Kopecks       int64
	PayerName     string
	AccountNumber string
	Address       *string
	PremisesKind  *string
	PremisesNo    *string
}

type PaymentExports struct{ pool *pgxpool.Pool }

func NewPaymentExports(pool *pgxpool.Pool) *PaymentExports { return &PaymentExports{pool} }

func (s *PaymentExports) List(ctx context.Context, f model.PaymentExportFilter, limit, offset int) ([]model.PaymentExport, error) {
	return collect[model.PaymentExport](s.pool.Query(ctx,
		`SELECT `+exportCols+` FROM payment_exports
		 WHERE ($1::bigint IS NULL OR bank_account_id = $1) AND (deleted_at IS NOT NULL) = $2
		 ORDER BY created_at DESC, id DESC LIMIT $3 OFFSET $4`,
		f.BankAccountID, f.Deleted, limit, offset))
}

func (s *PaymentExports) Get(ctx context.Context, id int64) (model.PaymentExport, error) {
	return one[model.PaymentExport](s.pool.Query(ctx, `SELECT `+exportCols+` FROM payment_exports WHERE id = $1`, id))
}

// File возвращает имя и содержимое файла реестра.
func (s *PaymentExports) File(ctx context.Context, id int64) (string, []byte, error) {
	var name string
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT file_name, file_content FROM payment_exports WHERE id = $1`, id).Scan(&name, &data)
	if err != nil {
		return "", nil, mapErr(err)
	}
	return name, data, nil
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// selectRows отбирает платежи для выгрузки в порядке даты. lock блокирует строки до конца транзакции.
func selectRows(ctx context.Context, q querier, f model.IncomingPaymentFilter, sel ExportSelection, lock bool) ([]exportRow, error) {
	if sel.Mode != "ids" && sel.Mode != "filter" {
		return nil, invalid("mode: want ids or filter")
	}
	f.Deleted = false
	ids := sel.IDs
	if ids == nil {
		ids = []int64{}
	}
	sql := `SELECT id, bank_account_id, payment_date, payment_time::text, round(amount * 100)::bigint, payer_name,
	               (SELECT a.number FROM personal_accounts a WHERE a.id = personal_account_id),
	               (SELECT b.address FROM personal_accounts a JOIN premises pr ON pr.id = a.premises_id JOIN buildings b ON b.id = pr.building_id WHERE a.id = personal_account_id),
	               (SELECT pr.kind FROM personal_accounts a JOIN premises pr ON pr.id = a.premises_id WHERE a.id = personal_account_id),
	               (SELECT pr.number FROM personal_accounts a JOIN premises pr ON pr.id = a.premises_id WHERE a.id = personal_account_id)
	          FROM incoming_payments
	         WHERE ` + incomingWhere + `
	           AND registry_id IS NULL AND personal_account_id IS NOT NULL AND export_id IS NULL
	           AND (($19::text = 'ids') = (id = ANY($20::bigint[])))
	         ORDER BY payment_date, payment_time NULLS FIRST, id LIMIT $12 OFFSET $13`
	if lock {
		sql += ` FOR UPDATE`
	}
	args := append(incomingArgs(f, maxExportRows+1, 0), sel.Mode, ids)
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []exportRow
	for rows.Next() {
		var r exportRow
		if err := rows.Scan(&r.ID, &r.BankAccountID, &r.Date, &r.Time, &r.Kopecks, &r.PayerName, &r.AccountNumber, &r.Address, &r.PremisesKind, &r.PremisesNo); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, r)
	}
	return out, mapErr(rows.Err())
}

// Preview показывает, сколько платежей и на какую сумму попадёт в реестр, и на каких банковских счетах они лежат.
func (s *PaymentExports) Preview(ctx context.Context, f model.IncomingPaymentFilter, sel ExportSelection) (ExportPreview, error) {
	rows, err := selectRows(ctx, s.pool, f, sel, false)
	if err != nil {
		return ExportPreview{}, err
	}
	p := ExportPreview{BankAccountIDs: []int64{}}
	var kopecks int64
	seen := map[int64]bool{}
	for _, r := range rows {
		kopecks += r.Kopecks
		if !seen[r.BankAccountID] {
			seen[r.BankAccountID] = true
			p.BankAccountIDs = append(p.BankAccountIDs, r.BankAccountID)
		}
	}
	p.Count, p.Total = len(rows), float64(kopecks)/100
	if p.Count > maxExportRows {
		return ExportPreview{}, errTooMany
	}
	return p, nil
}

var innRe = regexp.MustCompile(`^\d{10}(\d{2})?$`)

// premisesLabels — как в адресе строки реестра пишется вид помещения.
var premisesLabels = map[string]string{
	"apartment": "КВ.", "parking_space": "МАШИНОМЕСТО.", "storage": "КЛАДОВАЯ.", "non_residential": "НЕЖИЛОЕ.", "commercial": "КОММЕРЧЕСКОЕ.",
}

func (r exportRow) address() string {
	a := ""
	if r.Address != nil {
		a = *r.Address
	}
	if r.PremisesNo != nil && r.PremisesKind != nil {
		if a != "" {
			a += ", "
		}
		a += premisesLabels[*r.PremisesKind] + *r.PremisesNo
	}
	return a
}

// Create формирует реестр из выбранных платежей одной транзакцией и помечает платежи выгруженными.
// Все платежи должны относиться к одному банковскому счёту.
func (s *PaymentExports) Create(ctx context.Context, f model.IncomingPaymentFilter, sel ExportSelection) (model.PaymentExport, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.PaymentExport{}, mapErr(err)
	}
	defer rollback(ctx, tx)

	rows, err := selectRows(ctx, tx, f, sel, true)
	if err != nil {
		return model.PaymentExport{}, err
	}
	switch {
	case len(rows) == 0:
		return model.PaymentExport{}, invalid("no payments to export")
	case len(rows) > maxExportRows:
		return model.PaymentExport{}, errTooMany
	case sel.ExpectedCount != nil && *sel.ExpectedCount != len(rows):
		return model.PaymentExport{}, &Error{ErrConflict, fmt.Sprintf("selection changed: expected %d payments, found %d", *sel.ExpectedCount, len(rows))}
	}
	bankID := rows[0].BankAccountID
	for _, r := range rows {
		if r.BankAccountID != bankID {
			return model.PaymentExport{}, invalid("payments belong to several bank accounts")
		}
	}

	var number string
	var inn *string
	if err := tx.QueryRow(ctx, `SELECT ba.number, o.inn FROM bank_accounts ba JOIN organizations o ON o.id = ba.organization_id WHERE ba.id = $1`, bankID).Scan(&number, &inn); err != nil {
		return model.PaymentExport{}, mapErr(err)
	}
	if inn == nil || !innRe.MatchString(*inn) {
		return model.PaymentExport{}, invalid("organization inn must be 10 or 12 digits")
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, exportLockKey, int32(bankID)); err != nil {
		return model.PaymentExport{}, mapErr(err)
	}
	var next int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(number), 0) + 1 FROM payment_exports WHERE bank_account_id = $1`, bankID).Scan(&next); err != nil {
		return model.PaymentExport{}, mapErr(err)
	}

	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.UTC
	}
	reg := sberexport.Registry{OrgINN: *inn, BankAccount: number, Number: next, Date: time.Now().In(loc)}
	ids := make([]int64, 0, len(rows))
	var kopecks int64
	for _, r := range rows {
		row := sberexport.Row{ID: r.ID, Date: r.Date, Amount: r.Kopecks, PayerName: r.PayerName, AccountNumber: r.AccountNumber, Address: r.address()}
		if r.Time != nil {
			row.Time = *r.Time
		}
		reg.Rows = append(reg.Rows, row)
		ids = append(ids, r.ID)
		kopecks += r.Kopecks
	}
	data, err := reg.Build()
	if err != nil {
		return model.PaymentExport{}, err
	}
	exp, err := one[model.PaymentExport](tx.Query(ctx,
		`INSERT INTO payment_exports (bank_account_id, number, file_name, file_content, payments_count, total_amount)
		 VALUES ($1, $2, $3, $4, $5, $6::numeric / 100) RETURNING `+exportCols,
		bankID, next, reg.FileName(), data, len(rows), kopecks))
	if err != nil {
		return model.PaymentExport{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE incoming_payments SET export_id = $1, updated_at = now() WHERE id = ANY($2)`, exp.ID, ids); err != nil {
		return model.PaymentExport{}, mapErr(err)
	}
	return exp, mapErr(tx.Commit(ctx))
}

// Revoke отзывает выгрузку: платежи снова считаются невыгруженными, файл остаётся в корзине.
func (s *PaymentExports) Revoke(ctx context.Context, id int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return mapErr(err)
	}
	defer rollback(ctx, tx)
	tag, err := tx.Exec(ctx, `UPDATE payment_exports SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err := checkDeleted(tag, err); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE incoming_payments SET export_id = NULL, updated_at = now() WHERE export_id = $1`, id); err != nil {
		return mapErr(err)
	}
	return mapErr(tx.Commit(ctx))
}
