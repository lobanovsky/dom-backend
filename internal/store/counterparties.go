package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type Counterparties struct{ pool *pgxpool.Pool }

func NewCounterparties(pool *pgxpool.Pool) *Counterparties { return &Counterparties{pool} }

// List строит список контрагентов из платежей: получатели неудалённых исходящих и плательщики неудалённых входящих
// платежей из выписок (строки реестров не берутся: это жильцы, а не контрагенты банка). Ключ — ИНН и наименование
// без регистра, кавычек и пробелов. Порядок: недавно платившие первыми.
func (s *Counterparties) List(ctx context.Context, f model.CounterpartyFilter, limit, offset int) ([]model.Counterparty, error) {
	return collect[model.Counterparty](s.pool.Query(ctx,
		`WITH p AS (
		     SELECT 'out' AS dir, recipient_inn AS inn, recipient_name AS name, recipient_account AS account, recipient_bank_name AS bank_name, amount, payment_date
		       FROM outgoing_payments WHERE deleted_at IS NULL
		     UNION ALL
		     SELECT 'in', payer_inn, payer_name, payer_account, payer_bank_name, amount, payment_date
		       FROM incoming_payments WHERE deleted_at IS NULL AND registry_id IS NULL AND btrim(payer_name) <> ''
		 ), k AS (
		     SELECT *, coalesce(inn, '') AS inn_key, regexp_replace(lower(name), '[^[:alnum:]]+', '', 'g') AS name_key FROM p
		 )
		 SELECT mode() WITHIN GROUP (ORDER BY name) AS name,
		        nullif(max(inn), '') AS inn,
		        coalesce((array_agg(DISTINCT account) FILTER (WHERE account IS NOT NULL AND account <> ''))[1:5], '{}') AS accounts,
		        mode() WITHIN GROUP (ORDER BY bank_name) AS bank_name,
		        count(*) FILTER (WHERE dir = 'out')::int AS outgoing_count,
		        coalesce(sum(amount) FILTER (WHERE dir = 'out'), 0)::float8 AS outgoing_total,
		        count(*) FILTER (WHERE dir = 'in')::int AS incoming_count,
		        coalesce(sum(amount) FILTER (WHERE dir = 'in'), 0)::float8 AS incoming_total,
		        min(payment_date) AS first_payment, max(payment_date) AS last_payment
		   FROM k
		  GROUP BY inn_key, name_key
		 HAVING ($1 = '' OR count(*) FILTER (WHERE dir = $1) > 0)
		    AND ($2::text IS NULL OR concat_ws(' ', mode() WITHIN GROUP (ORDER BY name), max(inn), string_agg(DISTINCT account, ' ')) ILIKE $2)
		  ORDER BY max(payment_date) DESC, 1 LIMIT $3 OFFSET $4`,
		dirCode(f.Direction), likePattern(f.Q), limit, offset))
}

func dirCode(direction string) string {
	switch direction {
	case "outgoing":
		return "out"
	case "incoming":
		return "in"
	}
	return ""
}
