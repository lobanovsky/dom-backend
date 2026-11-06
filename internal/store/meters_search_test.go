package store

import (
	"context"
	"testing"

	"dom-backend/internal/model"
)

// Поиск счётчиков: q — только номер и название счётчика, номер помещения ищется отдельно и точно;
// счётчики одного помещения идут подряд, даже если номер помещения повторяется у разных видов.
func TestMetersSearchAndOrder(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "meters-search-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "meters-search-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	premises, meters := NewPremisesStore(pool), NewMeters(pool)
	newPremises := func(kind, number string) model.Premises {
		p, err := premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: kind, Number: number})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "premises", p.ID)
		return p
	}
	newMeter := func(p model.Premises, resource, serial string) model.Meter {
		m, err := meters.Create(ctx, model.Meter{PremisesID: &p.ID, Resource: resource, Tariffs: 1, SerialNumber: serial, Coefficient: 1, InstalledAt: date(2026, 1, 1)})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "meters", m.ID)
		return m
	}
	flat1, flat16, parking1 := newPremises("apartment", "1"), newPremises("apartment", "16"), newPremises("parking_space", "1")
	a1 := newMeter(flat1, "hot_water", "ms-test-0001")
	b16 := newMeter(flat16, "cold_water", "ms-test-7101")
	a2 := newMeter(flat1, "cold_water", "ms-test-0002")
	p1 := newMeter(parking1, "cold_water", "ms-test-0003")
	scope := ptr("individual")
	ids := func(f model.MeterFilter) []int64 {
		f.Scope, f.BuildingID = scope, &b.ID
		list, err := meters.List(ctx, f, 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		out := []int64{}
		for _, m := range list {
			out = append(out, m.ID)
		}
		return out
	}
	equal := func(got, want []int64) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}
	// «1» в q ищет только в номерах счётчиков (…0001 и …7101), а не квартиру 1
	if got := ids(model.MeterFilter{Q: ptr("1")}); !equal(got, []int64{a1.ID, b16.ID}) {
		t.Errorf("q=1: %v, want %v", got, []int64{a1.ID, b16.ID})
	}
	// точный номер помещения: «1» — квартира 1 и машиноместо 1, но не 16
	if got := ids(model.MeterFilter{PremisesNumber: ptr("1")}); len(got) != 3 || got[0] == b16.ID || got[1] == b16.ID || got[2] == b16.ID {
		t.Errorf("premises_number=1: %v", got)
	}
	if got := ids(model.MeterFilter{PremisesNumber: ptr("16")}); !equal(got, []int64{b16.ID}) {
		t.Errorf("premises_number=16: %v", got)
	}
	// порядок: помещения по номеру (1, 1, 16), счётчики помещения подряд; внутри помещения — по ресурсу (cold < hot)
	got := ids(model.MeterFilter{})
	pos := map[int64]int{}
	for i, id := range got {
		pos[id] = i
	}
	if pos[a2.ID]+1 != pos[a1.ID] && pos[a1.ID]+1 != pos[a2.ID] {
		t.Errorf("meters of flat 1 must be adjacent: %v", got)
	}
	if pos[p1.ID] > pos[b16.ID] || pos[b16.ID] != 3 {
		t.Errorf("order by premises number: %v", got)
	}
}
