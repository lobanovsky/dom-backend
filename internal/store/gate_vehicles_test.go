package store

import (
	"context"
	"dom-backend/internal/model"
	"testing"
)

func TestGateVehiclesAndCommercial(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	s := NewGate(pool)
	org, e := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "gate-vehicles"})
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, e := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "gate-vehicles"})
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "buildings", b.ID)
	p, e := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: "commercial", Number: "3"})
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "premises", p.ID)
	person, e := NewPersons(pool).Create(ctx, model.Person{FirstName: "Тест", LastName: "Собственник", Phones: []string{}, Emails: []string{}})
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "persons", person.ID)
	own, e := NewOwnerships(pool).Create(ctx, model.Ownership{PersonID: &person.ID, PremisesID: p.ID, ShareNum: 1, ShareDen: 1, ValidFrom: date(2020, 1, 1)})
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "ownerships", own.ID)
	props, e := s.Properties(ctx, person.ID)
	if e != nil || len(props) != 1 || props[0].Zone != "courtyard" {
		t.Fatalf("commercial courtyard: %+v %v", props, e)
	}
	in := model.GateBatchInput{PremisesIDs: []int64{p.ID}, Phone: "79990000001", RecipientName: "Тест", Relation: "owner", Vehicles: []model.GateVehicleInput{{PlateNumber: " а123аа77 ", Brand: "Audi"}, {PlateNumber: "В456ВВ77", Brand: "Toyota"}}}
	rows, e := s.CreateBatch(ctx, person.ID, in)
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "gate_requests", rows[0].ID)
	r := rows[0]
	if len(r.Vehicles) != 2 || r.Vehicles[0].PlateNumber != "А123АА77" || !r.Vehicles[1].Active || r.VehiclePlate != "А123АА77" {
		t.Fatalf("vehicles not preserved: %+v", r)
	}
	found, e := s.List(ctx, person.ID, model.GateFilter{Q: "В456ВВ77"}, 50, 0)
	if e != nil || len(found) != 1 {
		t.Fatalf("search second car: %+v %v", found, e)
	}
	// Неактивный автомобиль хранится для истории, но не считается текущим при поиске.
	if _, e = pool.Exec(ctx, `UPDATE gate_requests SET vehicles=jsonb_set(vehicles,'{1,active}','false') WHERE id=$1`, r.ID); e != nil {
		t.Fatal(e)
	}
	history, e := s.get(ctx, person.ID, r.ID)
	if e != nil || len(history.Vehicles) != 2 || history.Vehicles[1].Active {
		t.Fatalf("inactive car lost: %+v %v", history, e)
	}
	found, e = s.List(ctx, person.ID, model.GateFilter{Q: "В456ВВ77"}, 50, 0)
	if e != nil || len(found) != 0 {
		t.Fatalf("inactive car matched as current: %+v %v", found, e)
	}
}
