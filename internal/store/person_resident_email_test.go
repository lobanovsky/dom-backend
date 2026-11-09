package store

import (
	"context"
	"dom-backend/internal/model"
	"errors"
	"testing"
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
