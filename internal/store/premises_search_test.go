package store

import (
	"context"
	"testing"

	"dom-backend/internal/model"
)

// Поиск помещения по началу номера: точное совпадение идёт первым, даже если у помещений с более длинными
// номерами (50, 51…) меньше id.
func TestPremisesSearchPutsExactNumberFirst(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "search-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "search-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	premises := NewPremisesStore(pool)
	for _, number := range []string{"50", "51", "5", "6", "500"} {
		p, err := premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: number})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "premises", p.ID)
	}
	got, err := premises.List(ctx, model.PremisesFilter{BuildingID: &b.ID, Q: ptr("5")}, 10, 0)
	if err != nil || len(got) != 4 || got[0].Number != "5" {
		t.Fatalf("q=5: %+v, err = %v", got, err)
	}
	for _, p := range got {
		if p.Number == "6" {
			t.Errorf("a number that does not start with the query must not match: %+v", got)
		}
	}
	// без q порядок прежний (по id)
	all, err := premises.List(ctx, model.PremisesFilter{BuildingID: &b.ID}, 10, 0)
	if err != nil || len(all) != 5 || all[0].Number != "50" {
		t.Errorf("without q: %+v, err = %v", all, err)
	}
}

// Поиск по q находит помещение и по ФИО действующего собственника: слова в любом порядке, ё = е, прежние владельцы не в счёт.
func TestPremisesSearchByOwnerName(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "owner-search-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "owner-search-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	premises, persons, ownerships := NewPremisesStore(pool), NewPersons(pool), NewOwnerships(pool)
	newPremises := func(number string) model.Premises {
		p, err := premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: number})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "premises", p.ID)
		return p
	}
	own := func(p model.Premises, last, first string, to *model.Date) {
		person, err := persons.Create(ctx, model.Person{LastName: last, FirstName: first, Phones: []string{}, Emails: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "persons", person.ID)
		o, err := ownerships.Create(ctx, model.Ownership{PremisesID: p.ID, PersonID: &person.ID, ShareNum: 1, ShareDen: 2, ValidFrom: model.NewDate(2015, 1, 1), ValidTo: to})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "ownerships", o.ID)
	}
	one, two := newPremises("1"), newPremises("2")
	own(one, "Алёхин", "Пётр", nil)
	own(two, "Прежний", "Иван", ptr(model.NewDate(2020, 1, 1)))
	find := func(q string) []string {
		got, err := premises.List(ctx, model.PremisesFilter{BuildingID: &b.ID, Q: ptr(q)}, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		numbers := []string{}
		for _, p := range got {
			numbers = append(numbers, p.Number)
		}
		return numbers
	}
	for _, q := range []string{"алех", "Алёхин", "пётр алёхин", "ПЕТР АЛЕХИН"} {
		if got := find(q); len(got) != 1 || got[0] != "1" {
			t.Errorf("q=%q: %v", q, got)
		}
	}
	if got := find("прежний"); len(got) != 0 {
		t.Errorf("a former owner must not be found: %v", got)
	}
	if got := find("алёхин иван"); len(got) != 0 {
		t.Errorf("all words must match the same owner: %v", got)
	}
	if got := find("2"); len(got) != 1 || got[0] != "2" {
		t.Errorf("number search still works: %v", got)
	}
}
