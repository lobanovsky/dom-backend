package store

import (
	"context"
	"strings"
	"testing"

	"dom-backend/internal/model"
)

// Справочник видов: помещение принимает только действующий вид, вид нельзя удалить, пока он используется,
// и нельзя переименовать его код.
func TestPremisesKinds(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	kinds := NewPremisesKinds(pool)

	seeded, err := kinds.List(ctx, model.PremisesKindFilter{}, 50, 0)
	if err != nil || len(seeded) < 3 || seeded[0].Code != "apartment" {
		t.Fatalf("seeded kinds: %+v, err = %v", seeded, err)
	}

	k, err := kinds.Create(ctx, model.PremisesKind{Code: "kinds_test_garage", Name: "Гараж (тест)", Position: 99})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises_kinds", k.ID)
	if _, err := kinds.Create(ctx, model.PremisesKind{Code: "kinds_test_other", Name: "гараж (ТЕСТ)"}); err == nil {
		t.Error("name must be unique case-insensitively")
	}
	if _, err := kinds.Update(ctx, k.ID, model.PremisesKind{Code: "renamed", Name: "Гараж (тест)"}); err == nil || !strings.Contains(err.Error(), "code") {
		t.Errorf("code change: err = %v", err)
	}
	if k, err = kinds.Update(ctx, k.ID, model.PremisesKind{Code: k.Code, Name: "Гараж (тест 2)", Position: 98}); err != nil || k.Name != "Гараж (тест 2)" {
		t.Fatalf("rename: %+v, err = %v", k, err)
	}

	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "kinds-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "kinds-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	premises := NewPremisesStore(pool)

	if _, err := premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: "no_such_kind", Number: "1"}); err == nil || !strings.Contains(err.Error(), "premises kind") {
		t.Errorf("unknown kind: err = %v", err)
	}
	p, err := premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: k.Code, Number: "1"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises", p.ID)

	if err := kinds.Delete(ctx, k.ID); err == nil || !strings.Contains(err.Error(), "has active premises") {
		t.Errorf("delete used kind: err = %v", err)
	}
	if err := premises.Delete(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := kinds.Delete(ctx, k.ID); err != nil {
		t.Fatalf("delete unused kind: %v", err)
	}
	if _, err := premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: k.Code, Number: "2"}); err == nil {
		t.Error("a deleted kind must not be accepted")
	}
	if _, err := premises.Restore(ctx, p.ID); err == nil {
		t.Error("restoring premises of a deleted kind must fail")
	}
	if _, err := kinds.Restore(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := premises.Restore(ctx, p.ID); err != nil {
		t.Errorf("restore premises after restoring the kind: %v", err)
	}
}

// Правила вида помещения: у машиноместа нет проживающих и один собственник; правило нельзя включить
// для вида, помещения которого ему не отвечают, и нельзя обойти сменой вида помещения.
func TestPremisesKindRules(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	kinds := NewPremisesKinds(pool)

	seeded, err := kinds.List(ctx, model.PremisesKindFilter{}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range seeded {
		// у машиноместа нет жильцов, но собственников может быть несколько (migration 0036)
		if k.Code == "parking_space" && (!k.NoResidents || k.SingleOwner) {
			t.Errorf("parking_space must forbid residents and allow several owners: %+v", k)
		}
		if k.Code == "apartment" && (k.NoResidents || k.SingleOwner) {
			t.Errorf("apartment must have no rules: %+v", k)
		}
	}

	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "kind-rules-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "kind-rules-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	premises := NewPremisesStore(pool)
	newPremises := func(kind, number string) model.Premises {
		p, err := premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: kind, Number: number})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "premises", p.ID)
		return p
	}
	// вид с обоими правилами заводим сам: у справочного «Машиноместа» правило единственного собственника снято
	strict, err := kinds.Create(ctx, model.PremisesKind{Code: "kind_rules_strict", Name: "Строгий (тест)", Position: 998, NoResidents: true, SingleOwner: true})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises_kinds", strict.ID)
	parking, flat := newPremises(strict.Code, "1"), newPremises("apartment", "2")
	newPerson := func(last string) model.Person {
		p, err := NewPersons(pool).Create(ctx, model.Person{LastName: last, FirstName: "Тест", Phones: []string{}, Emails: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "persons", p.ID)
		return p
	}
	p1, p2 := newPerson("Правила-1"), newPerson("Правила-2")

	residencies, ownerships := NewResidencies(pool), NewOwnerships(pool)
	d := model.NewDate
	newResidency := func(premisesID int64) model.Residency {
		return model.Residency{PersonID: p1.ID, PremisesID: premisesID, Relation: "other", ValidFrom: d(2020, 1, 1)}
	}
	newOwnership := func(premisesID, personID int64, from model.Date, to *model.Date) model.Ownership {
		return model.Ownership{PremisesID: premisesID, PersonID: &personID, ShareNum: 1, ShareDen: 2, ValidFrom: from, ValidTo: to}
	}

	// проживающие
	if _, err := residencies.Create(ctx, newResidency(parking.ID)); err == nil || !strings.Contains(err.Error(), "does not allow residents") {
		t.Errorf("resident in a parking space: err = %v", err)
	}
	r, err := residencies.Create(ctx, newResidency(flat.ID))
	if err != nil {
		t.Fatalf("resident in an apartment: %v", err)
	}
	cleanup(t, pool, "residencies", r.ID)

	// один собственник: доли 1/2 не мешают сумме долей, но два владения одновременно запрещены
	o1, err := ownerships.Create(ctx, newOwnership(parking.ID, p1.ID, d(2020, 1, 1), ptr(d(2022, 12, 31))))
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "ownerships", o1.ID)
	if _, err := ownerships.Create(ctx, newOwnership(parking.ID, p2.ID, d(2022, 6, 1), nil)); err == nil || !strings.Contains(err.Error(), "single owner") {
		t.Errorf("overlapping owners of a parking space: err = %v", err)
	}
	o2, err := ownerships.Create(ctx, newOwnership(parking.ID, p2.ID, d(2023, 1, 1), nil))
	if err != nil {
		t.Fatalf("a new owner after the previous one: %v", err)
	}
	cleanup(t, pool, "ownerships", o2.ID)
	// квартира: двое владельцев одновременно разрешены
	a1, err := ownerships.Create(ctx, newOwnership(flat.ID, p1.ID, d(2020, 1, 1), nil))
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "ownerships", a1.ID)
	a2, err := ownerships.Create(ctx, newOwnership(flat.ID, p2.ID, d(2020, 1, 1), nil))
	if err != nil {
		t.Fatalf("two owners of an apartment: %v", err)
	}
	cleanup(t, pool, "ownerships", a2.ID)

	// смена вида помещения не должна обходить правила
	flat.Kind = strict.Code
	if _, err := premises.Update(ctx, flat.ID, flat); err == nil || !strings.Contains(err.Error(), "has residents") {
		t.Errorf("apartment with residents to a parking space: err = %v", err)
	}
	if err := residencies.Delete(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := premises.Update(ctx, flat.ID, flat); err == nil || !strings.Contains(err.Error(), "has several owners") {
		t.Errorf("apartment with two owners to a parking space: err = %v", err)
	}

	// правило нельзя включить для вида, помещения которого ему не отвечают
	k, err := kinds.Create(ctx, model.PremisesKind{Code: "kind_rules_test", Name: "Правила (тест)", Position: 999})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises_kinds", k.ID)
	custom := newPremises("kind_rules_test", "3")
	cr, err := residencies.Create(ctx, newResidency(custom.ID))
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "residencies", cr.ID)
	k.NoResidents = true
	if _, err := kinds.Update(ctx, k.ID, k); err == nil || !strings.Contains(err.Error(), "residents") {
		t.Errorf("enable no_residents with residents: err = %v", err)
	}
	if err := residencies.Delete(ctx, cr.ID); err != nil {
		t.Fatal(err)
	}
	if k, err = kinds.Update(ctx, k.ID, k); err != nil || !k.NoResidents {
		t.Fatalf("enable no_residents: %+v, err = %v", k, err)
	}
	if _, err := residencies.Restore(ctx, cr.ID); err == nil {
		t.Error("restoring a resident of a no-residents premises must fail")
	}
}
