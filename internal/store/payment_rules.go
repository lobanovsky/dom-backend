package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

const ruleCols = `id, name, position, enabled, direction, match_mode, conditions, action, created_at, updated_at, deleted_at`

type PaymentRules struct{ pool *pgxpool.Pool }

func NewPaymentRules(pool *pgxpool.Pool) *PaymentRules { return &PaymentRules{pool} }

func (s *PaymentRules) List(ctx context.Context, f model.PaymentRuleFilter, limit, offset int) ([]model.PaymentRule, error) {
	return collect[model.PaymentRule](s.pool.Query(ctx,
		`SELECT `+ruleCols+` FROM payment_rules
		 WHERE ($1::bool IS NULL OR enabled = $1) AND (deleted_at IS NOT NULL) = $2
		 ORDER BY position, id LIMIT $3 OFFSET $4`, f.Enabled, f.Deleted, limit, offset))
}

func (s *PaymentRules) Get(ctx context.Context, id int64) (model.PaymentRule, error) {
	return one[model.PaymentRule](s.pool.Query(ctx, `SELECT `+ruleCols+` FROM payment_rules WHERE id = $1`, id))
}

func (s *PaymentRules) Create(ctx context.Context, in model.PaymentRule) (model.PaymentRule, error) {
	if err := s.checkRefs(ctx, in.Action); err != nil {
		return model.PaymentRule{}, err
	}
	return one[model.PaymentRule](s.pool.Query(ctx,
		`INSERT INTO payment_rules (name, position, enabled, direction, match_mode, conditions, action)
		 VALUES ($1, COALESCE(NULLIF($2, 0), (SELECT COALESCE(MAX(position), 0) + 1 FROM payment_rules WHERE deleted_at IS NULL)), $3, $4, $5, $6, $7)
		 RETURNING `+ruleCols, in.Name, in.Position, in.Enabled, in.Direction, in.MatchMode, in.Conditions, in.Action))
}

// Update заменяет правило; позиция 0 в теле означает «не менять порядок».
func (s *PaymentRules) Update(ctx context.Context, id int64, in model.PaymentRule) (model.PaymentRule, error) {
	if err := s.checkRefs(ctx, in.Action); err != nil {
		return model.PaymentRule{}, err
	}
	return one[model.PaymentRule](s.pool.Query(ctx,
		`UPDATE payment_rules SET name = $2, position = CASE WHEN $3 = 0 THEN position ELSE $3 END, enabled = $4, direction = $5,
		        match_mode = $6, conditions = $7, action = $8, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING `+ruleCols,
		id, in.Name, in.Position, in.Enabled, in.Direction, in.MatchMode, in.Conditions, in.Action))
}

func (s *PaymentRules) Restore(ctx context.Context, id int64) (model.PaymentRule, error) {
	return one[model.PaymentRule](s.pool.Query(ctx,
		`UPDATE payment_rules SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL RETURNING `+ruleCols, id))
}

func (s *PaymentRules) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "payment_rules", id)
}

// Reorder задаёт порядок правил: позиция = номер в списке ids. Правила, которых нет в списке, уходят в конец.
func (s *PaymentRules) Reorder(ctx context.Context, ids []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE payment_rules SET position = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id, i+1); err != nil {
			return mapErr(err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE payment_rules SET position = position + $2 WHERE deleted_at IS NULL AND NOT (id = ANY($1))`, ids, len(ids)); err != nil {
		return mapErr(err)
	}
	return mapErr(tx.Commit(ctx))
}

// checkRefs проверяет, что помещение, лицевой счёт и категория из действия существуют и не удалены.
func (s *PaymentRules) checkRefs(ctx context.Context, a model.RuleAction) error {
	check := func(field, table string, id *int64, extra string) error {
		if id == nil {
			return nil
		}
		var one int
		err := s.pool.QueryRow(ctx, `SELECT 1 FROM `+table+` WHERE id = $1 AND deleted_at IS NULL`+extra, *id).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid(field + ": not found or deleted")
		}
		return mapErr(err)
	}
	if err := check("action.premises_id", "premises", a.PremisesID, ""); err != nil {
		return err
	}
	if err := check("action.personal_account_id", "personal_accounts", a.PersonalAccountID, ""); err != nil {
		return err
	}
	if err := check("action.category_id", "payment_categories", a.CategoryID, " AND direction = 'incoming'"); err != nil {
		return err
	}
	return check("action.building_id", "buildings", a.BuildingID, "")
}
