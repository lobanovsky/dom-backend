package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dom-backend/internal/model"
)

func TestServiceNorms(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	services, norms, buildings := NewServices(pool), NewServiceNorms(pool), NewBuildings(pool)

	orgs := NewOrganizations(pool)
	org, err := orgs.Create(ctx, model.Organization{Kind: "tsn", Name: "norms-test"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := orgs.Create(ctx, model.Organization{Kind: "tsn", Name: "norms-test-other"})
	if err != nil {
		t.Fatal(err)
	}
	b1, err := buildings.Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "norms-test-1"})
	if err != nil {
		t.Fatal(err)
	}
	b2, err := buildings.Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "norms-test-2"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := buildings.Create(ctx, model.Building{OrganizationID: other.ID, Kind: "apartment_building", Address: "norms-test-foreign"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM service_norms WHERE service_id IN (SELECT id FROM services WHERE organization_id = $1)`, org.ID)
		pool.Exec(ctx, `DELETE FROM service_tariffs WHERE service_id IN (SELECT id FROM services WHERE organization_id = $1)`, org.ID)
		pool.Exec(ctx, `DELETE FROM services WHERE organization_id = $1`, org.ID)
		pool.Exec(ctx, `DELETE FROM buildings WHERE id IN ($1, $2, $3)`, b1.ID, b2.ID, foreign.ID)
		pool.Exec(ctx, `DELETE FROM organizations WHERE id IN ($1, $2)`, org.ID, other.ID)
	})

	mk := func(name, unit, method string) (model.Service, error) {
		in := model.Service{OrganizationID: org.ID, Kind: "utility", Name: name, Unit: unit, CalcMethod: method, Active: true}
		in.SetDefaults()
		return services.Create(ctx, in)
	}
	if _, err := mk("Отопление в м2", "m2", "per_area_norm"); err == nil {
		t.Errorf("area-norm service in m2 must be rejected")
	}
	heat, err := mk("Отопление", "gcal", "per_area_norm")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := mk("Содержание", "m2", "per_area")
	if err != nil {
		t.Fatal(err)
	}

	add := func(svc, b int64, v float64, from model.Date, to *model.Date) (model.ServiceNorm, error) {
		return norms.Create(ctx, model.ServiceNorm{ServiceID: svc, BuildingID: b, Value: v, ValidFrom: from, ValidTo: to})
	}
	first, err := add(heat.ID, b1.ID, 0.011, date(2026, 1, 1), ptr(date(2026, 12, 31)))
	if err != nil {
		t.Fatal(err)
	}
	if first.BuildingAddress != "norms-test-1" || first.Value != 0.011 {
		t.Errorf("norm: %+v", first)
	}
	// Тот же период в другом доме допустим, в том же доме — конфликт.
	if _, err := add(heat.ID, b2.ID, 0.016, date(2026, 1, 1), nil); err != nil {
		t.Errorf("other building: %v", err)
	}
	if _, err := add(heat.ID, b1.ID, 0.012, date(2026, 6, 1), nil); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "service_norms_no_overlap") {
		t.Errorf("overlap: %v", err)
	}
	if _, err := add(heat.ID, b1.ID, 0.012, date(2027, 1, 1), nil); err != nil {
		t.Errorf("next period: %v", err)
	}
	// Норматив только у услуги «площадь × норматив» и только для дома её организации.
	if _, err := add(plain.ID, b1.ID, 0.011, date(2026, 1, 1), nil); err == nil || !strings.Contains(err.Error(), "service has no norms") {
		t.Errorf("plain service: %v", err)
	}
	if _, err := add(heat.ID, foreign.ID, 0.011, date(2026, 1, 1), nil); err == nil || !strings.Contains(err.Error(), "another organization") {
		t.Errorf("foreign building: %v", err)
	}

	list, err := norms.List(ctx, model.ServiceNormFilter{ServiceID: &heat.ID, BuildingID: &b1.ID}, 10, 0)
	if err != nil || len(list) != 2 || list[0].ValidFrom != date(2027, 1, 1) {
		t.Errorf("list: %+v, %v", list, err)
	}

	// Пока есть нормативы, способ расчёта не сменить, а услугу и дом не удалить.
	heat.CalcMethod, heat.Unit = "fixed", "month"
	if _, err := services.Update(ctx, heat.ID, heat); err == nil || !strings.Contains(err.Error(), "must stay an area-norm service") {
		t.Errorf("change method: %v", err)
	}
	if err := services.Delete(ctx, heat.ID); err == nil || !strings.Contains(err.Error(), "norms") {
		t.Errorf("delete service: %v", err)
	}
	if err := buildings.Delete(ctx, b1.ID); err == nil || !strings.Contains(err.Error(), "service norms") {
		t.Errorf("delete building: %v", err)
	}

	// Удалённый норматив освобождает период; восстановление при пересечении — конфликт.
	if err := norms.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	again, err := add(heat.ID, b1.ID, 0.013, date(2026, 3, 1), ptr(date(2026, 9, 30)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := norms.Restore(ctx, first.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("restore overlap: %v", err)
	}
	if err := norms.Delete(ctx, again.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := norms.Restore(ctx, first.ID); err != nil {
		t.Errorf("restore: %v", err)
	}
}
