package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
	"dom-backend/internal/sberexport"
)

// adjustmentOpBase — начало диапазона номеров операций корректировочных реестров: номер операции = база + id реестра.
// Номера настоящих платежей (id платежа, до 12 знаков) его не достигают, поэтому условный платёж не совпадёт с настоящим.
const adjustmentOpBase int64 = 900_000_000_000

const adjustmentCols = `a.id, a.created_at, a.updated_at, a.deleted_at, a.bank_account_id, a.personal_account_id, a.number, a.amount::float8 AS amount,
	a.payer_name, a.comment, a.file_name, a.resolved_at, pa.number AS account_number, pa.premises_id, pr.kind AS premises_kind,
	pr.number AS premises_number, b.name AS building_name, b.address AS building_address`

const adjustmentFrom = ` FROM adjustment_registries a
	JOIN personal_accounts pa ON pa.id = a.personal_account_id
	JOIN premises pr ON pr.id = pa.premises_id
	JOIN buildings b ON b.id = pr.building_id`

type AdjustmentRegistries struct{ pool *pgxpool.Pool }

func NewAdjustmentRegistries(pool *pgxpool.Pool) *AdjustmentRegistries {
	return &AdjustmentRegistries{pool}
}

func (s *AdjustmentRegistries) List(ctx context.Context, f model.AdjustmentRegistryFilter, limit, offset int) ([]model.AdjustmentRegistry, error) {
	return collect[model.AdjustmentRegistry](s.pool.Query(ctx,
		`SELECT `+adjustmentCols+adjustmentFrom+`
		 WHERE ($1::bigint IS NULL OR a.bank_account_id = $1) AND ($2::bigint IS NULL OR a.personal_account_id = $2)
		   AND ($3::bool IS NULL OR (a.resolved_at IS NOT NULL) = $3) AND (a.deleted_at IS NOT NULL) = $4
		 ORDER BY a.created_at DESC, a.id DESC LIMIT $5 OFFSET $6`,
		f.BankAccountID, f.PersonalAccountID, f.Resolved, f.Deleted, limit, offset))
}

func (s *AdjustmentRegistries) Get(ctx context.Context, id int64) (model.AdjustmentRegistry, error) {
	return one[model.AdjustmentRegistry](s.pool.Query(ctx, `SELECT `+adjustmentCols+adjustmentFrom+` WHERE a.id = $1`, id))
}

// File возвращает имя и содержимое файла реестра.
func (s *AdjustmentRegistries) File(ctx context.Context, id int64) (string, []byte, error) {
	var name string
	var data []byte
	if err := s.pool.QueryRow(ctx, `SELECT file_name, file_content FROM adjustment_registries WHERE id = $1`, id).Scan(&name, &data); err != nil {
		return "", nil, mapErr(err)
	}
	return name, data, nil
}

// Create формирует реестр с одним условным платежом и сохраняет его в журнал. Платежи в системе не создаются.
func (s *AdjustmentRegistries) Create(ctx context.Context, req model.AdjustmentRegistryRequest) (model.AdjustmentRegistry, error) {
	kopecks := int64(math.Round(req.Amount * 100))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.AdjustmentRegistry{}, mapErr(err)
	}
	defer rollback(ctx, tx)

	var accountNumber, address, kind, premisesNo string
	err = tx.QueryRow(ctx,
		`SELECT pa.number, b.address, pr.kind, pr.number
		   FROM personal_accounts pa JOIN premises pr ON pr.id = pa.premises_id JOIN buildings b ON b.id = pr.building_id
		  WHERE pa.id = $1 AND pa.deleted_at IS NULL`, req.PersonalAccountID).Scan(&accountNumber, &address, &kind, &premisesNo)
	if err != nil {
		return model.AdjustmentRegistry{}, invalidRef(err, "personal_account_id: not found")
	}
	var bankNumber string
	var inn *string
	err = tx.QueryRow(ctx,
		`SELECT ba.number, o.inn FROM bank_accounts ba JOIN organizations o ON o.id = ba.organization_id
		  WHERE ba.id = $1 AND ba.deleted_at IS NULL`, req.BankAccountID).Scan(&bankNumber, &inn)
	if err != nil {
		return model.AdjustmentRegistry{}, invalidRef(err, "bank_account_id: not found")
	}
	if inn == nil || !innRe.MatchString(*inn) {
		return model.AdjustmentRegistry{}, invalid("organization inn must be 10 or 12 digits")
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, exportLockKey, int32(req.BankAccountID)); err != nil {
		return model.AdjustmentRegistry{}, mapErr(err)
	}
	var next int
	if err := tx.QueryRow(ctx, nextRegistryNumberSQL, req.BankAccountID).Scan(&next); err != nil {
		return model.AdjustmentRegistry{}, mapErr(err)
	}
	var id int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO adjustment_registries (bank_account_id, personal_account_id, number, amount, payer_name, comment, file_name, file_content)
		 VALUES ($1, $2, $3, $4::numeric / 100, $5, $6, '', ''::bytea) RETURNING id`,
		req.BankAccountID, req.PersonalAccountID, next, kopecks, req.PayerName, req.Comment).Scan(&id); err != nil {
		return model.AdjustmentRegistry{}, mapErr(err)
	}

	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	reg := sberexport.Registry{OrgINN: *inn, BankAccount: bankNumber, Number: next, Date: now, Rows: []sberexport.Row{{
		ID: adjustmentOpBase + id, Date: now, Time: now.Format("15:04:05"), Amount: kopecks, PayerName: req.PayerName,
		AccountNumber: accountNumber, Address: address + ", " + premisesLabel(kind) + premisesNo,
	}}}
	data, err := reg.Build()
	if err != nil {
		return model.AdjustmentRegistry{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE adjustment_registries SET file_name = $2, file_content = $3 WHERE id = $1`, id, reg.FileName(), data); err != nil {
		return model.AdjustmentRegistry{}, mapErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.AdjustmentRegistry{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

// SetResolved ставит (resolved=true) или снимает отметку «условный платёж убран из внешней системы».
func (s *AdjustmentRegistries) SetResolved(ctx context.Context, id int64, resolved bool) (model.AdjustmentRegistry, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE adjustment_registries SET resolved_at = CASE WHEN $2 THEN COALESCE(resolved_at, now()) END, updated_at = now()
		  WHERE id = $1 AND deleted_at IS NULL`, id, resolved)
	if err := checkDeleted(tag, err); err != nil {
		return model.AdjustmentRegistry{}, err
	}
	return s.Get(ctx, id)
}

// Delete убирает реестр из журнала (мягкое удаление).
func (s *AdjustmentRegistries) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE adjustment_registries SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return checkDeleted(tag, err)
}

// Restore возвращает удалённый реестр в журнал.
func (s *AdjustmentRegistries) Restore(ctx context.Context, id int64) (model.AdjustmentRegistry, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE adjustment_registries SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	if err := checkDeleted(tag, err); err != nil {
		return model.AdjustmentRegistry{}, err
	}
	return s.Get(ctx, id)
}

// invalidRef: запись, на которую ссылается запрос, не найдена — ошибка запроса (422), а не «не найдено».
func invalidRef(err error, msg string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return invalid(msg)
	}
	return mapErr(err)
}
