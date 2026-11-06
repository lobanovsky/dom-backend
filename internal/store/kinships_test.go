package store

import (
	"context"
	"strings"
	"testing"

	"dom-backend/internal/model"
)

// Справочник родства: проживание принимает только действующую запись, запись нельзя удалить,
// пока ею пользуются, и нельзя переименовать её код.
func TestKinships(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	kinships := NewKinships(pool)

	seeded, err := kinships.List(ctx, model.KinshipFilter{}, 100, 0)
	if err != nil || len(seeded) < 22 || seeded[0].Code != "owner" {
		t.Fatalf("seeded kinships: %d items, first %+v, err = %v", len(seeded), seeded[0], err)
	}

	k, err := kinships.Create(ctx, model.Kinship{Code: "kinship_test_cousin", Name: "Двоюродный брат (тест)", Position: 999})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "kinships", k.ID)
	if _, err := kinships.Create(ctx, model.Kinship{Code: "kinship_test_other", Name: "двоюродный БРАТ (тест)"}); err == nil {
		t.Error("name must be unique case-insensitively")
	}
	if _, err := kinships.Update(ctx, k.ID, model.Kinship{Code: "renamed", Name: k.Name}); err == nil || !strings.Contains(err.Error(), "code") {
		t.Errorf("code change: err = %v", err)
	}
	if k, err = kinships.Update(ctx, k.ID, model.Kinship{Code: k.Code, Name: "Двоюродный брат (тест 2)", Position: 998}); err != nil || k.Name != "Двоюродный брат (тест 2)" {
		t.Fatalf("rename: %+v, err = %v", k, err)
	}

	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "kinship-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "kinship-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	p, err := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "1"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises", p.ID)
	person, err := NewPersons(pool).Create(ctx, model.Person{LastName: "Родство", FirstName: "Тест", Phones: []string{}, Emails: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "persons", person.ID)

	residencies := NewResidencies(pool)
	newResidency := func(relation string) model.Residency {
		return model.Residency{PersonID: person.ID, PremisesID: p.ID, Relation: relation, ValidFrom: model.NewDate(2020, 1, 1)}
	}

	if _, err := residencies.Create(ctx, newResidency("no_such_kinship")); err == nil || !strings.Contains(err.Error(), "kinship") {
		t.Errorf("unknown kinship: err = %v", err)
	}
	r, err := residencies.Create(ctx, newResidency(k.Code))
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "residencies", r.ID)

	if err := kinships.Delete(ctx, k.ID); err == nil || !strings.Contains(err.Error(), "has active residencies") {
		t.Errorf("delete used kinship: err = %v", err)
	}
	if err := residencies.Delete(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := kinships.Delete(ctx, k.ID); err != nil {
		t.Fatalf("delete unused kinship: %v", err)
	}
	if _, err := residencies.Create(ctx, newResidency(k.Code)); err == nil {
		t.Error("a deleted kinship must not be accepted")
	}
	if _, err := residencies.Restore(ctx, r.ID); err == nil {
		t.Error("restoring a residency with a deleted kinship must fail")
	}
	if _, err := kinships.Restore(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := residencies.Restore(ctx, r.ID); err != nil {
		t.Errorf("restore residency after restoring the kinship: %v", err)
	}
}
