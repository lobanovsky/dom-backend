package store

import (
	"context"
	"dom-backend/internal/model"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestGateLifecycleAndPermissions(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	s := NewGate(pool)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "gate-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "gate-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	person := func(name string) model.Person {
		v, e := NewPersons(pool).Create(ctx, model.Person{FirstName: name, LastName: "Gate", Phones: []string{}, Emails: []string{}})
		if e != nil {
			t.Fatal(e)
		}
		cleanup(t, pool, "persons", v.ID)
		return v
	}
	owner, other := person("Owner"), person("Other")
	property := func(kind, number string) model.Premises {
		p, e := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: kind, Number: number})
		if e != nil {
			t.Fatal(e)
		}
		cleanup(t, pool, "premises", p.ID)
		return p
	}
	apartment, parking, foreign := property("apartment", "1"), property("parking_space", "1"), property("apartment", "2")
	own := func(p model.Premises, person model.Person) model.Ownership {
		v, e := NewOwnerships(pool).Create(ctx, model.Ownership{PremisesID: p.ID, PersonID: &person.ID, ShareNum: 1, ShareDen: 1, ValidFrom: date(2020, 1, 1)})
		if e != nil {
			t.Fatal(e)
		}
		cleanup(t, pool, "ownerships", v.ID)
		return v
	}
	apartmentOwn := own(apartment, owner)
	if _, err := pool.Exec(ctx, `UPDATE ownerships SET share_den=2 WHERE id=$1`, apartmentOwn.ID); err != nil {
		t.Fatal(err)
	}
	own(parking, owner)
	own(foreign, other)
	guard, err := NewUsers(pool).Create(ctx, "gate-test-guard", "unused", model.RoleGuard)
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "users", guard.ID)
	input := model.GateInput{PremisesID: apartment.ID, Zone: "courtyard", Phone: "8 999 123 45 67", RecipientName: "Гость", Relation: "guest"}
	create := func(in model.GateInput) model.GateRequest {
		r, e := s.Create(ctx, owner.ID, in)
		if e != nil {
			t.Fatal(e)
		}
		cleanup(t, pool, "gate_requests", r.ID)
		return r
	}
	props, err := s.Properties(ctx, owner.ID)
	if err != nil || len(props) != 2 {
		t.Fatalf("properties: %+v %v", props, err)
	}
	r := create(input)
	if r.Active || r.Status != "pending" || r.Phone != "79991234567" {
		t.Fatalf("submitted: %+v", r)
	}
	searched, err := s.List(ctx, 0, model.GateFilter{Q: "+7 (999) 123-45-67", Status: "pending"}, 50, 0)
	if err != nil || len(searched) != 1 || searched[0].ID != r.ID {
		t.Fatalf("formatted phone search: %+v %v", searched, err)
	}
	duplicate := input
	duplicate.Phone = "+7 (999) 123-45-67"
	if _, err = s.Create(ctx, owner.ID, duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	for _, wrong := range []model.GateInput{{PremisesID: foreign.ID, Zone: "courtyard", Phone: input.Phone, RecipientName: "X", Relation: "guest"}, {PremisesID: apartment.ID, Zone: "parking", Phone: input.Phone, RecipientName: "X", Relation: "guest"}, {PremisesID: parking.ID, Zone: "courtyard", Phone: input.Phone, RecipientName: "X", Relation: "guest"}} {
		if _, err = s.Create(ctx, owner.ID, wrong); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unauthorized property: %v", err)
		}
	}
	if _, err = s.Act(ctx, owner.ID, model.User{}, r.ID, "approve", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("self approval: %v", err)
	}
	if _, err = s.Act(ctx, other.ID, model.User{}, r.ID, "cancel", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign action: %v", err)
	}
	if _, err = s.Events(ctx, other.ID, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign history: %v", err)
	}
	list, err := s.List(ctx, other.ID, model.GateFilter{}, 50, 0)
	if err != nil || len(list) != 0 {
		t.Fatalf("foreign list: %+v %v", list, err)
	}
	if _, err = s.Act(ctx, 0, guard, r.ID, "reject", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reason required: %v", err)
	}
	r, err = s.Act(ctx, 0, guard, r.ID, "approve", "Проверено")
	if err != nil || !r.Active {
		t.Fatalf("approve: %+v %v", r, err)
	}
	if _, err = s.Act(ctx, 0, guard, r.ID, "reject", "Слишком поздно"); !errors.Is(err, ErrConflict) {
		t.Fatalf("redecision: %v", err)
	}
	r, err = s.Act(ctx, owner.ID, model.User{}, r.ID, "revoke", "")
	if err != nil || r.Active || r.Status != "revoked" {
		t.Fatalf("revoke: %+v %v", r, err)
	}
	events, err := s.Events(ctx, owner.ID, r.ID)
	if err != nil || len(events) != 3 || events[0].Action != "submitted" || events[1].ActorRole != "guard" || events[2].Action != "revoked" {
		t.Fatalf("audit: %+v %v", events, err)
	}
	r = create(input)
	r, err = s.Act(ctx, owner.ID, model.User{}, r.ID, "cancel", "")
	if err != nil || r.Status != "cancelled" {
		t.Fatalf("cancel: %+v %v", r, err)
	}
	// Один и тот же телефон допустим на другом собственном объекте/в другой зоне.
	parkInput := input
	parkInput.PremisesID = parking.ID
	parkInput.Zone = "parking"
	parkInput.VehiclePlate = ""
	parkInput.VehicleBrand = ""
	parkInput.OccasionalVehicle = true
	park := create(parkInput)
	r = create(input)
	if _, err = s.Act(ctx, 0, guard, r.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE ownerships SET valid_to='2020-02-01' WHERE id=$1`, apartmentOwn.ID); err != nil {
		t.Fatal(err)
	}
	r, err = s.get(ctx, owner.ID, r.ID)
	if err != nil || r.Active || r.EffectiveStatus != "ineligible" {
		t.Fatalf("ownership loss: %+v %v", r, err)
	}
	// Ожидающая заявка также проверяется заново при одобрении.
	if _, err = pool.Exec(ctx, `UPDATE gate_requests SET status='pending' WHERE id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Act(ctx, 0, guard, r.ID, "approve", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("lost ownership approval: %v", err)
	}
	if _, err = s.Act(ctx, 0, guard, park.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE gate_requests SET valid_until=($1::date-1) WHERE id=$2`, model.Date{Time: time.Now().UTC()}, park.ID); err != nil {
		t.Fatal(err)
	}
	park, err = s.get(ctx, owner.ID, park.ID)
	if err != nil || park.EffectiveStatus != "expired" || park.Active {
		t.Fatalf("expiry: %+v %v", park, err)
	}
	if _, err = s.Act(ctx, 0, guard, park.ID, "approve", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired approval: %v", err)
	}
	pendingExpiry := parkInput
	pendingExpiry.Phone = "79991234570"
	pending := create(pendingExpiry)
	if _, err = pool.Exec(ctx, `UPDATE gate_requests SET valid_until='2020-01-01' WHERE id=$1`, pending.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Act(ctx, 0, guard, pending.ID, "approve", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired pending approval: %v", err)
	}
	expired := parkInput
	expired.ValidUntil = ptr(date(2020, 1, 1))
	if _, err = s.Create(ctx, owner.ID, expired); !errors.Is(err, ErrInvalid) {
		t.Fatalf("past date: %v", err)
	}
	// Два сотрудника не могут принять разные решения по одной заявке.
	raceInput := parkInput
	raceInput.Phone = "79991234568"
	race := create(raceInput)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, action := range []string{"approve", "reject"} {
		wg.Add(1)
		go func(a string) {
			defer wg.Done()
			_, e := s.Act(ctx, 0, guard, race.ID, a, "Проверено")
			results <- e
		}(action)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent decisions: %d/%d", success, conflict)
	}
}

func TestGateConcurrentCreation(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	s := NewGate(pool)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "gate-concurrent"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "gate-concurrent"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	p, err := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "1"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises", p.ID)
	person, err := NewPersons(pool).Create(ctx, model.Person{FirstName: "Test", LastName: "Gate", Phones: []string{}, Emails: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "persons", person.ID)
	o, err := NewOwnerships(pool).Create(ctx, model.Ownership{PremisesID: p.ID, PersonID: &person.ID, ShareNum: 1, ShareDen: 1, ValidFrom: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "ownerships", o.ID)
	in := model.GateInput{PremisesID: p.ID, Zone: "courtyard", Phone: "79991234569", RecipientName: "Guest", Relation: "guest"}
	type result struct {
		r   model.GateRequest
		err error
	}
	out := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, e := s.Create(ctx, person.ID, in); out <- result{r, e} }()
	}
	wg.Wait()
	close(out)
	success, conflict := 0, 0
	for v := range out {
		if v.err == nil {
			success++
			cleanup(t, pool, "gate_requests", v.r.ID)
		} else if errors.Is(v.err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(v.err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent duplicate prevention: %d/%d", success, conflict)
	}
}
