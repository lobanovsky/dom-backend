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
		`SELECT id, last_name, first_name, middle_name, birth_date, phone, email, created_at, updated_at FROM persons
		 WHERE ($1::text IS NULL OR last_name = $1)
		   AND ($2::text IS NULL OR phone = $2)
		   AND ($3::text IS NULL OR concat_ws(' ', last_name, first_name, middle_name) ILIKE $3 OR phone ILIKE $3)
		 ORDER BY last_name, first_name, id LIMIT $4 OFFSET $5`, f.LastName, f.Phone, likePattern(f.Q), limit, offset))
}

func (s *Persons) Get(ctx context.Context, id int64) (model.Person, error) {
	return one[model.Person](s.pool.Query(ctx,
		`SELECT id, last_name, first_name, middle_name, birth_date, phone, email, created_at, updated_at FROM persons WHERE id = $1`, id))
}

func (s *Persons) Create(ctx context.Context, in model.Person) (model.Person, error) {
	return one[model.Person](s.pool.Query(ctx,
		`INSERT INTO persons (last_name, first_name, middle_name, birth_date, phone, email) VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, last_name, first_name, middle_name, birth_date, phone, email, created_at, updated_at`, in.LastName, in.FirstName, in.MiddleName, in.BirthDate, in.Phone, in.Email))
}

func (s *Persons) Update(ctx context.Context, id int64, in model.Person) (model.Person, error) {
	return one[model.Person](s.pool.Query(ctx,
		`UPDATE persons SET last_name = $2, first_name = $3, middle_name = $4, birth_date = $5, phone = $6, email = $7, updated_at = now()
		 WHERE id = $1 RETURNING id, last_name, first_name, middle_name, birth_date, phone, email, created_at, updated_at`, id, in.LastName, in.FirstName, in.MiddleName, in.BirthDate, in.Phone, in.Email))
}

func (s *Persons) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM persons WHERE id = $1`, id)
	return checkDeleted(tag, err)
}
