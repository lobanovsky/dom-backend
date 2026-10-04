package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type Persons struct{ pool *pgxpool.Pool }

func NewPersons(pool *pgxpool.Pool) *Persons { return &Persons{pool} }

func (s *Persons) List(ctx context.Context, f model.PersonFilter, limit, offset int) ([]model.Person, error) {
	return collect[model.Person](s.pool.Query(ctx,
		`SELECT id, last_name, first_name, middle_name, birth_date, phones, emails, created_at, updated_at, deleted_at FROM persons
		 WHERE ($1::text IS NULL OR last_name = $1)
		   AND ($2::text IS NULL OR $2 = ANY(phones))
		   AND ($3::text IS NULL
		        OR concat_ws(' ', last_name, first_name, middle_name) ILIKE $3
		        OR array_to_string(phones, ' ') ILIKE $3
		        OR array_to_string(emails, ' ') ILIKE $3)
		   AND (deleted_at IS NOT NULL) = $4
		 ORDER BY last_name, first_name, id LIMIT $5 OFFSET $6`, f.LastName, f.Phone, likePattern(f.Q), f.Deleted, limit, offset))
}

func (s *Persons) Get(ctx context.Context, id int64) (model.Person, error) {
	return one[model.Person](s.pool.Query(ctx,
		`SELECT id, last_name, first_name, middle_name, birth_date, phones, emails, created_at, updated_at, deleted_at FROM persons WHERE id = $1`, id))
}

func (s *Persons) Create(ctx context.Context, in model.Person) (model.Person, error) {
	return one[model.Person](s.pool.Query(ctx,
		`INSERT INTO persons (last_name, first_name, middle_name, birth_date, phones, emails) VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, last_name, first_name, middle_name, birth_date, phones, emails, created_at, updated_at, deleted_at`, in.LastName, in.FirstName, in.MiddleName, in.BirthDate, in.Phones, in.Emails))
}

func (s *Persons) Update(ctx context.Context, id int64, in model.Person) (model.Person, error) {
	return one[model.Person](s.pool.Query(ctx,
		`UPDATE persons SET last_name = $2, first_name = $3, middle_name = $4, birth_date = $5, phones = $6, emails = $7, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING id, last_name, first_name, middle_name, birth_date, phones, emails, created_at, updated_at, deleted_at`, id, in.LastName, in.FirstName, in.MiddleName, in.BirthDate, in.Phones, in.Emails))
}

// Restore снимает отметку удаления. Если родительская запись удалена или номер уже
// занят, БД вернёт ошибку (триггеры целостности и уникальные индексы).
func (s *Persons) Restore(ctx context.Context, id int64) (model.Person, error) {
	return one[model.Person](s.pool.Query(ctx,
		`UPDATE persons SET deleted_at = NULL, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NOT NULL RETURNING id, last_name, first_name, middle_name, birth_date, phones, emails, created_at, updated_at, deleted_at`, id))
}

func (s *Persons) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE persons SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return checkDeleted(tag, err)
}
