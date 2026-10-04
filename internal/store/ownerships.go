package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type Ownerships struct{ pool *pgxpool.Pool }

func NewOwnerships(pool *pgxpool.Pool) *Ownerships { return &Ownerships{pool} }

func (s *Ownerships) List(ctx context.Context, f model.OwnershipFilter, limit, offset int) ([]model.Ownership, error) {
	return collect[model.Ownership](s.pool.Query(ctx,
		`SELECT id, premises_id, person_id, legal_entity_id, share_num, share_den, valid_from, valid_to, basis, created_at, updated_at FROM ownerships
		 WHERE ($1::bigint IS NULL OR premises_id = $1)
		   AND ($2::bigint IS NULL OR person_id = $2)
		   AND ($3::bigint IS NULL OR legal_entity_id = $3)
		 ORDER BY id LIMIT $4 OFFSET $5`, f.PremisesID, f.PersonID, f.LegalEntityID, limit, offset))
}

func (s *Ownerships) Get(ctx context.Context, id int64) (model.Ownership, error) {
	return one[model.Ownership](s.pool.Query(ctx,
		`SELECT id, premises_id, person_id, legal_entity_id, share_num, share_den, valid_from, valid_to, basis, created_at, updated_at FROM ownerships WHERE id = $1`, id))
}

func (s *Ownerships) Create(ctx context.Context, in model.Ownership) (model.Ownership, error) {
	return s.write(ctx, `INSERT INTO ownerships (premises_id, person_id, legal_entity_id, share_num, share_den, valid_from, valid_to, basis) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, premises_id, person_id, legal_entity_id, share_num, share_den, valid_from, valid_to, basis, created_at, updated_at`, in.PremisesID, in.PersonID, in.LegalEntityID, in.ShareNum, in.ShareDen, in.ValidFrom, in.ValidTo, in.Basis)
}

func (s *Ownerships) Update(ctx context.Context, id int64, in model.Ownership) (model.Ownership, error) {
	return s.write(ctx, `UPDATE ownerships SET premises_id = $2, person_id = $3, legal_entity_id = $4, share_num = $5, share_den = $6, valid_from = $7, valid_to = $8, basis = $9, updated_at = now()
		 WHERE id = $1 RETURNING id, premises_id, person_id, legal_entity_id, share_num, share_den, valid_from, valid_to, basis, created_at, updated_at`, id, in.PremisesID, in.PersonID, in.LegalEntityID, in.ShareNum, in.ShareDen, in.ValidFrom, in.ValidTo, in.Basis)
}

// write выполняет запись и проверку суммы долей в одной транзакции.
func (s *Ownerships) write(ctx context.Context, q string, args ...any) (model.Ownership, error) {
	var zero model.Ownership
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer rollback(ctx, tx)

	item, err := one[model.Ownership](tx.Query(ctx, q, args...))
	if err != nil {
		return zero, err
	}
	if err := checkOwnershipShares(ctx, tx, item.ID); err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, mapErr(err)
	}
	return item, nil
}

func (s *Ownerships) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM ownerships WHERE id = $1`, id)
	return checkDeleted(tag, err)
}

// ListByPremises возвращает все владения помещения вместе с названием владельца.
func (s *Ownerships) ListByPremises(ctx context.Context, premisesID int64) ([]model.OwnershipView, error) {
	return collect[model.OwnershipView](s.pool.Query(ctx,
		`SELECT o.id, o.premises_id, o.person_id, o.legal_entity_id, o.share_num, o.share_den,
		        o.valid_from, o.valid_to, o.basis, o.created_at, o.updated_at,
		        CASE WHEN o.person_id IS NOT NULL THEN 'person' ELSE 'legal_entity' END AS owner_kind,
		        COALESCE(concat_ws(' ', p.last_name, p.first_name, p.middle_name), le.name) AS owner_name
		 FROM ownerships o
		 LEFT JOIN persons p ON p.id = o.person_id
		 LEFT JOIN legal_entities le ON le.id = o.legal_entity_id
		 WHERE o.premises_id = $1
		 ORDER BY o.valid_from, o.id`, premisesID))
}

// checkOwnershipShares проверяет, что на любую дату сумма долей по помещению не превышает 1.
// Сумма меняется только на датах начала владения, поэтому проверяем начало записи
// и начала остальных владений внутри её периода.
func checkOwnershipShares(ctx context.Context, tx pgx.Tx, id int64) error {
	const q = `
WITH me AS (
    SELECT premises_id, valid_from, valid_to, share_num::numeric / share_den AS share
    FROM ownerships WHERE id = $1
), points AS (
    SELECT me.valid_from AS d FROM me
    UNION
    SELECT o.valid_from FROM ownerships o, me
    WHERE o.premises_id = me.premises_id AND o.id <> $1
      AND o.valid_from >= me.valid_from
      AND (me.valid_to IS NULL OR o.valid_from <= me.valid_to)
)
SELECT EXISTS (
    SELECT 1 FROM points p, me
    WHERE me.share + COALESCE((
        SELECT sum(o.share_num::numeric / o.share_den) FROM ownerships o
        WHERE o.premises_id = me.premises_id AND o.id <> $1
          AND o.valid_from <= p.d AND (o.valid_to IS NULL OR o.valid_to >= p.d)
    ), 0) > 1
)`
	var exceeded bool
	if err := tx.QueryRow(ctx, q, id).Scan(&exceeded); err != nil {
		return mapErr(err)
	}
	if exceeded {
		return invalid("sum of ownership shares for the premises exceeds 1")
	}
	return nil
}
