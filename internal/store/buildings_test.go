package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dom-backend/internal/model"
)

func TestBuildingName(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "bname-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	buildings := NewBuildings(pool)
	mk := func(addr string, name *string) (model.Building, error) {
		return buildings.Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: addr, Name: name})
	}
	a, err := mk("bname-test-1", ptr("Паркинг"))
	if err != nil || a.Name == nil || *a.Name != "Паркинг" {
		t.Fatalf("create: %+v, %v", a, err)
	}
	cleanup(t, pool, "buildings", a.ID)
	// Без названия домов может быть сколько угодно.
	for _, addr := range []string{"bname-test-2", "bname-test-3"} {
		b, err := mk(addr, nil)
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "buildings", b.ID)
	}
	// Название уникально без учёта регистра.
	if _, err := mk("bname-test-4", ptr("паркинг")); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "buildings_name_uq") {
		t.Errorf("duplicate name: %v", err)
	}
	// Удалённый дом название освобождает.
	if err := buildings.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	b, err := mk("bname-test-5", ptr("паркинг"))
	if err != nil {
		t.Fatalf("name of deleted building: %v", err)
	}
	cleanup(t, pool, "buildings", b.ID)
	if _, err := buildings.Restore(ctx, a.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("restore with taken name: %v", err)
	}
}
