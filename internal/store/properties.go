package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type Properties struct{ pool *pgxpool.Pool }

func NewProperties(pool *pgxpool.Pool) *Properties { return &Properties{pool} }

func (s *Properties) ByPerson(ctx context.Context, id int64) ([]model.OwnedPremises, error) {
	return s.list(ctx, "person_id", id)
}

func (s *Properties) ByLegalEntity(ctx context.Context, id int64) ([]model.OwnedPremises, error) {
	return s.list(ctx, "legal_entity_id", id)
}

// list возвращает помещения, которыми владелец владеет на сегодня (действующие записи, период включает текущую дату).
// col — имя колонки владельца из кода, не из запроса.
func (s *Properties) list(ctx context.Context, col string, id int64) ([]model.OwnedPremises, error) {
	return collect[model.OwnedPremises](s.pool.Query(ctx,
		`SELECT o.id AS ownership_id, p.id AS premises_id, p.kind AS premises_kind, p.number AS premises_number,
		        p.total_area, b.id AS building_id, b.address AS building_address,
		        o.share_num, o.share_den, o.valid_from, o.valid_to
		 FROM ownerships o
		 JOIN premises p ON p.id = o.premises_id
		 JOIN buildings b ON b.id = p.building_id
		 WHERE o.`+col+` = $1 AND o.deleted_at IS NULL AND p.deleted_at IS NULL
		   AND o.valid_from <= CURRENT_DATE AND (o.valid_to IS NULL OR o.valid_to >= CURRENT_DATE)
		 ORDER BY b.address, p.kind, NULLIF(regexp_replace(p.number, '\D', '', 'g'), '')::int NULLS LAST, p.number, o.id`, id))
}
