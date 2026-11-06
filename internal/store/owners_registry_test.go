package store

import (
	"context"
	"testing"
	"time"

	"dom-backend/internal/model"
)

func TestRegistryLabelAndOrder(t *testing.T) {
	apt := "Квартира"
	for _, c := range []struct{ kind, number, want string }{
		{"apartment", "1", "кв.001"}, {"apartment", "144", "кв.144"}, {"parking_space", "7", "мм.007"},
		{"commercial", "3", "оф.003"}, {"apartment", "5А", "кв.5А"}, {"storage", "2", "storage.002"},
	} {
		if got := RegistryLabel(c.kind, nil, c.number); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.kind, c.number, got, c.want)
		}
	}
	if got := RegistryLabel("custom", &apt, "9"); got != "квартира 009" {
		t.Errorf("custom kind name: %q", got)
	}
	if !naturalLess("2", "10") || naturalLess("10", "2") || !naturalLess("5", "5А") || !naturalLess("9", "А") {
		t.Error("naturalLess")
	}
}

// Реестр: только действующие на дату собственники, порядок квартиры → коммерческие → машиноместа, номера по возрастанию.
func TestOwnersRegistryBuild(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "registry-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "registry-test, 1"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	premises, persons, ownerships := NewPremisesStore(pool), NewPersons(pool), NewOwnerships(pool)
	newPremises := func(kind, number string, a float64) model.Premises {
		p, err := premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: kind, Number: number, TotalArea: &a})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "premises", p.ID)
		return p
	}
	own := func(p model.Premises, last string, from model.Date, to *model.Date) {
		person, err := persons.Create(ctx, model.Person{LastName: last, FirstName: "Тест", Phones: []string{}, Emails: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "persons", person.ID)
		o, err := ownerships.Create(ctx, model.Ownership{PremisesID: p.ID, PersonID: &person.ID, ShareNum: 1, ShareDen: 2, ValidFrom: from, ValidTo: to})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "ownerships", o.ID)
	}
	mm, flat10, flat2 := newPremises("parking_space", "1", 13.2), newPremises("apartment", "10", 60), newPremises("apartment", "2", 40)
	own(mm, "Машина", model.NewDate(2020, 1, 1), nil)
	own(flat2, "Прежний", model.NewDate(2015, 1, 1), ptr(model.NewDate(2020, 12, 31)))
	own(flat2, "Новый", model.NewDate(2021, 1, 1), nil)
	own(flat10, "Будущий", model.NewDate(2030, 1, 1), nil)

	reg, err := NewOwnersRegistry(pool).Build(ctx, b.ID, time.Date(2026, 11, 6, 0, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if reg.Address != "registry-test, 1" || reg.Day != "2026-11-06" || len(reg.Rows) != 3 {
		t.Fatalf("registry: %+v", reg)
	}
	if got := []string{reg.Rows[0].Label, reg.Rows[1].Label, reg.Rows[2].Label}; got[0] != "кв.002" || got[1] != "кв.010" || got[2] != "мм.001" {
		t.Errorf("order: %v", got)
	}
	if len(reg.Rows[0].Owners) != 1 || reg.Rows[0].Owners[0] != "Новый Тест" {
		t.Errorf("owners on the date: %v", reg.Rows[0].Owners)
	}
	if len(reg.Rows[1].Owners) != 0 {
		t.Errorf("a future owner must not appear: %v", reg.Rows[1].Owners)
	}
	if reg.TotalArea() != 113.2 {
		t.Errorf("total area = %v", reg.TotalArea())
	}
	if _, err := NewOwnersRegistry(pool).Build(ctx, 1<<40, time.Now()); err == nil {
		t.Error("unknown building must fail")
	}
}
