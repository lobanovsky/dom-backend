package store

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

var nonDigits = regexp.MustCompile(`\D`)

type Persons struct{ pool *pgxpool.Pool }

func NewPersons(pool *pgxpool.Pool) *Persons { return &Persons{pool} }

func (s *Persons) List(ctx context.Context, f model.PersonFilter, limit, offset int) ([]model.Person, error) {
	return collect[model.Person](s.pool.Query(ctx,
		`SELECT persons.id, last_name, first_name, middle_name, birth_date, phones, emails, persons.created_at, persons.updated_at, persons.deleted_at FROM persons LEFT JOIN resident_access ra ON ra.person_id=persons.id LEFT JOIN resident_accounts a ON a.person_id=persons.id
		 WHERE ($1::text IS NULL OR last_name = $1)
		   AND ($2::text IS NULL OR $2 = ANY(phones))
		   AND ($3::text IS NULL
		        OR concat_ws(' ', last_name, first_name, middle_name) ILIKE $3
		        OR array_to_string(phones, ' ') ILIKE $3
		        OR ($7::text IS NOT NULL AND array_to_string(phones, ' ') ILIKE $7)
		        OR array_to_string(emails, ' ') ILIKE $3)
		   AND (persons.deleted_at IS NOT NULL) = $4
		   AND ($8::text IS NULL OR CASE
		     WHEN ra.person_id IS NULL THEN 'unissued'
		     WHEN NOT ra.enabled OR (a.id IS NOT NULL AND NOT a.active) THEN 'disabled'
		     WHEN NOT EXISTS(SELECT 1 FROM ownerships o JOIN premises rp ON rp.id=o.premises_id JOIN buildings b ON b.id=rp.building_id JOIN organizations org ON org.id=b.organization_id WHERE o.person_id=persons.id AND o.deleted_at IS NULL AND rp.deleted_at IS NULL AND b.deleted_at IS NULL AND org.deleted_at IS NULL AND o.valid_from <= (now() AT TIME ZONE 'Europe/Moscow')::date AND (o.valid_to IS NULL OR o.valid_to >= (now() AT TIME ZONE 'Europe/Moscow')::date)) THEN 'unavailable'
		     WHEN a.id IS NULL THEN 'approved'
		     WHEN a.last_login_at IS NULL THEN 'registered'
		     ELSE 'used' END = $8)
		 ORDER BY last_name, first_name, id LIMIT $5 OFFSET $6`, f.LastName, f.Phone, likePattern(f.Q), f.Deleted, limit, offset, phoneDigitsPattern(f.Q), f.ResidentStatus))
}

// phoneDigitsPattern — шаблон ILIKE по цифрам запроса: «+7 (963) 611» находит 79636110726 (телефоны хранятся без форматирования).
// Меньше трёх цифр — шаблона нет (иначе «1» нашёл бы половину базы); ведущая 8 читается как 7.
func phoneDigitsPattern(q *string) *string {
	if q == nil {
		return nil
	}
	digits := nonDigits.ReplaceAllString(*q, "")
	if len(digits) < 3 {
		return nil
	}
	if digits[0] == '8' {
		digits = "7" + digits[1:]
	}
	p := "%" + digits + "%"
	return &p
}

func (s *Persons) Get(ctx context.Context, id int64) (model.Person, error) {
	return one[model.Person](s.pool.Query(ctx,
		`SELECT persons.id, last_name, first_name, middle_name, birth_date, phones, emails, persons.created_at, persons.updated_at, persons.deleted_at FROM persons WHERE id = $1`, id))
}

func (s *Persons) Create(ctx context.Context, in model.Person) (model.Person, error) {
	return one[model.Person](s.pool.Query(ctx,
		`INSERT INTO persons (last_name, first_name, middle_name, birth_date, phones, emails) VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, last_name, first_name, middle_name, birth_date, phones, emails, created_at, updated_at, deleted_at`, in.LastName, in.FirstName, in.MiddleName, in.BirthDate, in.Phones, in.Emails))
}

func (s *Persons) Update(ctx context.Context, id int64, in model.Person) (model.Person, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Person{}, err
	}
	defer rollback(ctx, tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('resident:' || $1::bigint::text,0))`, id); err != nil {
		return model.Person{}, err
	}
	oldLogin, err := emailForPerson(ctx, tx, `SELECT email FROM resident_accounts WHERE person_id=$1 FOR UPDATE`, id)
	if err != nil {
		return model.Person{}, err
	}
	oldAccessEmail, err := emailForPerson(ctx, tx, `SELECT email FROM resident_access WHERE person_id=$1 FOR UPDATE`, id)
	if err != nil {
		return model.Person{}, err
	}

	var p model.Person
	err = tx.QueryRow(ctx,
		`UPDATE persons SET last_name = $2, first_name = $3, middle_name = $4, birth_date = $5, phones = $6, emails = $7, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING id, last_name, first_name, middle_name, birth_date, phones, emails, created_at, updated_at, deleted_at`, id, in.LastName, in.FirstName, in.MiddleName, in.BirthDate, in.Phones, in.Emails).Scan(
		&p.ID, &p.LastName, &p.FirstName, &p.MiddleName, &p.BirthDate, &p.Phones, &p.Emails, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt)
	if err != nil {
		return model.Person{}, mapErr(err)
	}

	if len(in.Emails) == 0 {
		if oldLogin != nil || oldAccessEmail != nil {
			return model.Person{}, invalid("resident account requires a primary email")
		}
	} else {
		newLogin := in.Emails[0]
		loginChanged := (oldLogin != nil && !strings.EqualFold(strings.TrimSpace(*oldLogin), strings.TrimSpace(newLogin))) ||
			(oldAccessEmail != nil && !strings.EqualFold(strings.TrimSpace(*oldAccessEmail), strings.TrimSpace(newLogin)))
		if oldLogin != nil && loginChanged {
			_, err = tx.Exec(ctx, `UPDATE resident_accounts SET email=lower(btrim($2)) WHERE person_id=$1`, id, newLogin)
			if err != nil {
				return model.Person{}, mapErr(err)
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE resident_access SET email=lower(btrim($2)),updated_at=now() WHERE person_id=$1`, id, newLogin); err != nil {
			return model.Person{}, mapErr(err)
		}
		if loginChanged {
			if _, err = tx.Exec(ctx, `DELETE FROM resident_tokens WHERE person_id=$1`, id); err != nil {
				return model.Person{}, err
			}
			for _, previous := range []*string{oldLogin, oldAccessEmail} {
				if previous != nil {
					if _, err = tx.Exec(ctx, `DELETE FROM resident_mail_outbox WHERE email=$1`, *previous); err != nil {
						return model.Person{}, err
					}
				}
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return model.Person{}, mapErr(err)
	}
	return p, nil
}

func emailForPerson(ctx context.Context, tx pgx.Tx, query string, id int64) (*string, error) {
	var email string
	err := tx.QueryRow(ctx, query, id).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &email, nil
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
