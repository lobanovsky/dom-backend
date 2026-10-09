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
