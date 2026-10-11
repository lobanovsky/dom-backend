package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Реестр — детализация одного платежа из выписки: банк переводит сумму реестра одним платежом, а в назначении пишет
// имя файла реестра («//РЕЕСТР// 081026_VTB_231068_002.TXT,КОЛ-ВО-3», «ЭЛ.РЕЕСТР EPS…_900005_…_445.txt»).
// payment_registries.payment_id указывает на этот платёж. Условия сопоставления:
//   - тот же банковский счёт, платёж не удалён и сам не из реестра;
//   - имя файла реестра входит в назначение платежа (без учёта регистра);
//   - сумма платежа равна «к перечислению» из итоговой строки реестра (страховка от случайных вхождений короткого
//     имени в длинное: 9715357654_…_445.txt внутри 900005_9715357654_…_445.txt);
//   - связь однозначна в обе стороны: у реестра один кандидат и у платежа один реестр.
//
// Тот же запрос, но без привязки к счёту, выполнен в миграции 0011.
const registryCandidates = `
	SELECT r.id AS registry_id, p.id AS payment_id,
	       count(*) OVER (PARTITION BY r.id) AS per_registry, count(*) OVER (PARTITION BY p.id) AS per_payment
	  FROM payment_registries r
	  JOIN incoming_payments p ON p.bank_account_id = r.bank_account_id AND p.registry_id IS NULL AND p.deleted_at IS NULL
	   AND p.amount = r.total_transferred AND position(lower(r.file_name) IN lower(coalesce(p.purpose, ''))) > 0
	 WHERE r.payment_id IS NULL AND r.deleted_at IS NULL AND ($1::bigint IS NULL OR r.bank_account_id = $1)
	   AND NOT EXISTS (SELECT 1 FROM payment_registries x WHERE x.payment_id = p.id)`

// linkRegistries связывает реестры (одного счёта или всех, если bankAccountID == nil) с платежами выписок и возвращает число новых связей.
func linkRegistries(ctx context.Context, tx pgx.Tx, bankAccountID *int64) (int, error) {
	tag, err := tx.Exec(ctx,
		`WITH cand AS (`+registryCandidates+`)
		 UPDATE payment_registries r SET payment_id = c.payment_id, updated_at = now()
		   FROM cand c WHERE c.registry_id = r.id AND c.per_registry = 1 AND c.per_payment = 1`, bankAccountID)
	if err != nil {
		return 0, mapErr(err)
	}
	return int(tag.RowsAffected()), nil
}

// Причины, по которым реестр остался без платежа.
const (
	UnmatchedNoPayment = "no_payment" // платёж в выписках не найден (выписка ещё не загружена или сумма не сошлась)
	UnmatchedAmbiguous = "ambiguous"  // подходит несколько платежей или платёж подходит нескольким реестрам
)

type UnmatchedRegistry struct {
	RegistryID int64  `json:"registry_id" db:"registry_id"`
	FileName   string `json:"file_name" db:"file_name"`
	Reason     string `json:"reason" db:"reason"`
}

// Relink повторяет сопоставление для всех реестров (кнопка «Сопоставить») и возвращает число новых связей и реестры без платежа.
func (s *PaymentRegistries) Relink(ctx context.Context) (int, []UnmatchedRegistry, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer rollback(ctx, tx)
	linked, err := linkRegistries(ctx, tx, nil)
	if err != nil {
		return 0, nil, err
	}
	unmatched, err := collect[UnmatchedRegistry](tx.Query(ctx,
		`WITH cand AS (`+registryCandidates+`)
		 SELECT r.id AS registry_id, r.file_name,
		        CASE WHEN EXISTS (SELECT 1 FROM cand c WHERE c.registry_id = r.id) THEN 'ambiguous' ELSE 'no_payment' END AS reason
		   FROM payment_registries r WHERE r.payment_id IS NULL AND r.deleted_at IS NULL ORDER BY r.registry_date DESC NULLS LAST, r.id DESC`, nil))
	if err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, mapErr(err)
	}
	return linked, unmatched, nil
}
