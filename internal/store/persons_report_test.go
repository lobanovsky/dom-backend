package store

import (
	"context"
	"testing"
	"time"

	"dom-backend/internal/model"
)

// Люди идут по первому своему объекту: квартиры → коммерческие → машиноместа; объекты человека в том же порядке;
// прежние владельцы, юрлица и удалённые не попадают.
func TestPersonsReportBuild(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "persons-report-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "persons-report-test, 1"})
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
	newPerson := func(last string) model.Person {
		p, err := persons.Create(ctx, model.Person{LastName: last, FirstName: "Тест", Phones: []string{}, Emails: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "persons", p.ID)
		return p
	}
	own := func(p model.Premises, person model.Person, num, den int, to *model.Date) {
		from := model.NewDate(2015, 1, 1)
		if to != nil {
			from = model.NewDate(2005, 1, 1) // прежний владелец: до начала действующего
		}
		o, err := ownerships.Create(ctx, model.Ownership{PremisesID: p.ID, PersonID: &person.ID, ShareNum: num, ShareDen: den, ValidFrom: from, ValidTo: to})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "ownerships", o.ID)
	}
	flat2, flat10, shop, mm := newPremises("apartment", "2", 40), newPremises("apartment", "10", 60), newPremises("commercial", "3", 30), newPremises("parking_space", "1", 13.2)
	a, bb, c, former := newPerson("Атестов"), newPerson("Бтестов"), newPerson("Втестов"), newPerson("Прежнев")
	own(mm, a, 1, 1, nil)                                    // у А машиноместо и квартира 10
	own(flat10, a, 1, 2, nil)                                //
	own(flat2, bb, 1, 1, nil)                                // у Б квартира 2: он первый
	own(shop, c, 1, 1, nil)                                  // у В коммерческое
	own(flat2, former, 1, 1, ptr(model.NewDate(2010, 1, 1))) // не владеет на сегодня

	rep, err := NewPersonsReport(pool).Build(ctx, time.Date(2026, 11, 7, 0, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	// в базе могут быть и настоящие данные: берём только наших тестовых людей
	var mine []model.PersonsReportPerson
	for _, p := range rep.Persons {
		for _, n := range []string{"Атестов Тест", "Бтестов Тест", "Втестов Тест", "Прежнев Тест"} {
			if p.Name == n {
				mine = append(mine, p)
			}
		}
	}
	if len(mine) != 3 || mine[0].Name != "Бтестов Тест" || mine[1].Name != "Атестов Тест" || mine[2].Name != "Втестов Тест" {
		t.Fatalf("order: %+v", mine)
	}
	if got := mine[1].Items; len(got) != 2 || got[0].Label != "кв.010" || got[0].ShareDen != 2 || got[1].Label != "мм.001" {
		t.Errorf("items of А: %+v", got)
	}
}
