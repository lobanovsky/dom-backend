package store

import (
	"context"
	"testing"

	"dom-backend/internal/model"
)

// Телефоны хранятся цифрами: поиск q находит их по цифрам запроса в любом написании, а при коротком запросе не ищет по телефонам.
func TestPersonsPhoneSearchByDigits(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	persons := NewPersons(pool)
	p, err := persons.Create(ctx, model.Person{LastName: "Телефонов-тест", FirstName: "Пётр", Phones: []string{"79636110726"}, Emails: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "persons", p.ID)

	found := func(f model.PersonFilter) bool {
		list, err := persons.List(ctx, f, 200, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range list {
			if x.ID == p.ID {
				return true
			}
		}
		return false
	}
	for _, q := range []string{"79636110726", "+7 (963) 611-07-26", "+7 (963) 611", "89636110726", "963611", "8 963"} {
		if !found(model.PersonFilter{Q: ptr(q)}) {
			t.Errorf("q=%q must find the person", q)
		}
	}
	for _, q := range []string{"7963611999", "+7 (926)"} {
		if found(model.PersonFilter{Q: ptr(q)}) {
			t.Errorf("q=%q must not find the person", q)
		}
	}
	if !found(model.PersonFilter{Phone: ptr("79636110726")}) || found(model.PersonFilter{Phone: ptr("79636110727")}) {
		t.Error("phone filter is an exact match on the stored digits")
	}
}
