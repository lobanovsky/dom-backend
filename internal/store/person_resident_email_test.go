package store

import (
	"context"
	"dom-backend/internal/model"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestPersonResidentEmailConflicts(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	s := NewPersons(pool)
	first, err := s.Create(ctx, model.Person{LastName: "Email", FirstName: "Первый", Phones: []string{}, Emails: []string{"portal-unique@example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "persons", first.ID)
	_, err = s.Create(ctx, model.Person{LastName: "Email", FirstName: "Дубликат", Phones: []string{}, Emails: []string{"PORTAL-UNIQUE@example.test"}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate primary accepted: %v", err)
	}
	second, err := s.Create(ctx, model.Person{LastName: "Email", FirstName: "Второй", Phones: []string{}, Emails: []string{"another-unique@example.test", "portal-unique@example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "persons", second.ID)
	second.Emails = []string{"portal-unique@example.test", "another-unique@example.test"}
	if _, err = s.Update(ctx, second.ID, second); !errors.Is(err, ErrInvalid) {
		t.Fatalf("conflicting selection accepted: %v", err)
	}
	second.Emails = []string{}
	if _, err = s.Update(ctx, second.ID, second); err != nil {
		t.Fatalf("removing all emails: %v", err)
	}
}

func TestPersonPrimaryEmailUpdatesResidentLogin(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	s := NewPersons(pool)
	person, err := s.Create(ctx, model.Person{LastName: "Email", FirstName: "Кабинет", Phones: []string{}, Emails: []string{"login-old@example.test", "login-new@example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	username := fmt.Sprintf("resident-email-%d", time.Now().UnixNano())
	var admin int64
	if err = pool.QueryRow(ctx, `INSERT INTO users(username,password_hash,role) VALUES($1,'unused','admin') RETURNING id`, username).Scan(&admin); err != nil {
		t.Fatal(err)
	}
	var account int64
	if err = pool.QueryRow(ctx, `INSERT INTO resident_accounts(person_id,email,password_hash) VALUES($1,'login-old@example.test','hash') RETURNING id`, person.ID).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO resident_access(person_id,email,enabled,approved_by) VALUES($1,'login-old@example.test',true,$2)`, person.ID, admin); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO resident_tokens(token_hash,person_id,email,purpose,expires_at) VALUES('resident-email-test-token',$1,'login-old@example.test','reset',now()+interval '1 hour')`, person.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO resident_sessions(token_hash,account_id,expires_at) VALUES('resident-email-test-session',$1,now()+interval '1 hour')`, account); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO resident_mail_outbox(email,link,expires_at) VALUES('login-old@example.test','https://example.test',now()+interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, statement := range []struct {
			query string
			args  []any
		}{
			{`DELETE FROM resident_sessions WHERE account_id=$1`, []any{account}},
			{`DELETE FROM resident_tokens WHERE person_id=$1`, []any{person.ID}},
			{`DELETE FROM resident_mail_outbox WHERE email='login-old@example.test'`, nil},
			{`DELETE FROM resident_access WHERE person_id=$1`, []any{person.ID}},
			{`DELETE FROM resident_accounts WHERE person_id=$1`, []any{person.ID}},
			{`DELETE FROM persons WHERE id=$1`, []any{person.ID}},
			{`DELETE FROM users WHERE id=$1`, []any{admin}},
		} {
			if _, e := pool.Exec(context.Background(), statement.query, statement.args...); e != nil {
				t.Errorf("cleanup: %v", e)
			}
		}
	})

	person.Emails = []string{"login-new@example.test", "login-old@example.test"}
	if _, err = s.Update(ctx, person.ID, person); err != nil {
		t.Fatal(err)
	}
	var accountEmail, accessEmail string
	if err = pool.QueryRow(ctx, `SELECT email FROM resident_accounts WHERE person_id=$1`, person.ID).Scan(&accountEmail); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT email FROM resident_access WHERE person_id=$1`, person.ID).Scan(&accessEmail); err != nil {
		t.Fatal(err)
	}
	if accountEmail != "login-new@example.test" || accessEmail != accountEmail {
		t.Fatalf("login was not updated: account=%q access=%q", accountEmail, accessEmail)
	}
	var tokens, outbox, sessions int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM resident_tokens WHERE person_id=$1`, person.ID).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM resident_mail_outbox WHERE email='login-old@example.test'`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM resident_sessions WHERE account_id=$1`, account).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if tokens != 0 || outbox != 0 || sessions != 1 {
		t.Fatalf("unexpected login change side effects: tokens=%d outbox=%d sessions=%d", tokens, outbox, sessions)
	}
}
