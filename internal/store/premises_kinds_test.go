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
	if err != nil || len(seeded) < 5 || seeded[0].Code != "apartment" {
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
