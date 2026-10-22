package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type PremisesStore struct{ pool *pgxpool.Pool }

func NewPremisesStore(pool *pgxpool.Pool) *PremisesStore { return &PremisesStore{pool} }

// List возвращает помещения; owner_names — ФИО физлиц и названия юрлиц, чьё владение
// не закончилось (valid_from не учитываем: у импортированных данных это дата импорта).
func (s *PremisesStore) List(ctx context.Context, f model.PremisesFilter, limit, offset int) ([]model.PremisesView, error) {
	return collect[model.PremisesView](s.pool.Query(ctx,
		`SELECT pr.id, pr.building_id, pr.kind, pr.number, pr.entrance, pr.floor, pr.total_area, pr.living_area, pr.rooms, pr.cadastral_number, pr.created_at, pr.updated_at, pr.deleted_at,
		        ARRAY(SELECT COALESCE(concat_ws(' ', p.last_name, p.first_name, p.middle_name), le.name)
		              FROM ownerships o
		              LEFT JOIN persons p ON p.id = o.person_id
		              LEFT JOIN legal_entities le ON le.id = o.legal_entity_id
		              WHERE o.premises_id = pr.id AND o.deleted_at IS NULL
		                AND (o.valid_to IS NULL OR o.valid_to >= current_date)
		              ORDER BY o.id) AS owner_names
		 FROM premises pr
		 WHERE ($1::bigint IS NULL OR pr.building_id = $1)
		   AND ($2::text IS NULL OR pr.kind = $2)
		   AND ($3::text IS NULL OR pr.number = $3)
		   AND ($4::text IS NULL OR pr.number ILIKE $4)
		   AND (pr.deleted_at IS NOT NULL) = $5
		 ORDER BY (pr.number ILIKE $8) DESC NULLS LAST, pr.id LIMIT $6 OFFSET $7`, // при поиске по q точное совпадение номера идёт первым
		f.BuildingID, f.Kind, f.Number, prefixPattern(f.Q), f.Deleted, limit, offset, exactPattern(f.Q)))
}

func (s *PremisesStore) Get(ctx context.Context, id int64) (model.Premises, error) {
	return one[model.Premises](s.pool.Query(ctx,
		`SELECT id, building_id, kind, number, entrance, floor, total_area, living_area, rooms, cadastral_number, created_at, updated_at, deleted_at FROM premises WHERE id = $1`, id))
}

func (s *PremisesStore) Create(ctx context.Context, in model.Premises) (model.Premises, error) {
	return one[model.Premises](s.pool.Query(ctx,
		`INSERT INTO premises (building_id, kind, number, entrance, floor, total_area, living_area, rooms, cadastral_number) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, building_id, kind, number, entrance, floor, total_area, living_area, rooms, cadastral_number, created_at, updated_at, deleted_at`, in.BuildingID, in.Kind, in.Number, in.Entrance, in.Floor, in.TotalArea, in.LivingArea, in.Rooms, in.CadastralNumber))
}

func (s *PremisesStore) Update(ctx context.Context, id int64, in model.Premises) (model.Premises, error) {
	return one[model.Premises](s.pool.Query(ctx,
		`UPDATE premises SET building_id = $2, kind = $3, number = $4, entrance = $5, floor = $6, total_area = $7, living_area = $8, rooms = $9, cadastral_number = $10, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING id, building_id, kind, number, entrance, floor, total_area, living_area, rooms, cadastral_number, created_at, updated_at, deleted_at`, id, in.BuildingID, in.Kind, in.Number, in.Entrance, in.Floor, in.TotalArea, in.LivingArea, in.Rooms, in.CadastralNumber))
}

// Restore снимает отметку удаления. Если родительская запись удалена или номер уже
// занят, БД вернёт ошибку (триггеры целостности и уникальные индексы).
func (s *PremisesStore) Restore(ctx context.Context, id int64) (model.Premises, error) {
	return one[model.Premises](s.pool.Query(ctx,
		`UPDATE premises SET deleted_at = NULL, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NOT NULL RETURNING id, building_id, kind, number, entrance, floor, total_area, living_area, rooms, cadastral_number, created_at, updated_at, deleted_at`, id))
}

func (s *PremisesStore) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE premises SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return checkDeleted(tag, err)
}
