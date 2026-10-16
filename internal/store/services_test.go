package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dom-backend/internal/model"
)

func TestServicesAndTariffs(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	services, tariffs := NewServices(pool), NewServiceTariffs(pool)

	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "services-test"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "services-test-other"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{org.ID, other.ID} {
		id := id
		t.Cleanup(func() {
			pool.Exec(ctx, `DELETE FROM service_components WHERE service_id IN (SELECT id FROM services WHERE organization_id = $1)`, id)
			pool.Exec(ctx, `DELETE FROM service_tariffs WHERE service_id IN (SELECT id FROM services WHERE organization_id = $1)`, id)
			pool.Exec(ctx, `DELETE FROM services WHERE organization_id = $1`, id)
			pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, id)
		})
	}

	res := func(s string) *string { return &s }
	tar := func(n int) *int { return &n }
	make := func(in model.Service) model.Service {
		t.Helper()
		in.Active = true
		in.SetDefaults()
		out, err := services.Create(ctx, in)
		if err != nil {
			t.Fatalf("create %s: %v", in.Name, err)
		}
		return out
	}
	cold := make(model.Service{OrganizationID: org.ID, Kind: "utility", Name: "ХВС", Unit: "m3", CalcMethod: "individual_meter", MeterResource: res("cold_water")})
	hot := make(model.Service{OrganizationID: org.ID, Kind: "utility", Name: "ХВС для ГВС", Unit: "m3", CalcMethod: "individual_meter", MeterResource: res("hot_water")})
	e2 := make(model.Service{OrganizationID: org.ID, Kind: "utility", Name: "Электроэнергия Т2", Unit: "kwh", CalcMethod: "common_meter", MeterResource: res("electricity"), MeterTariff: tar(2)})
	if *cold.MeterTariff != 1 || cold.OrganizationName != "services-test" || cold.CurrentPrice != nil || len(cold.ComponentIDs) != 0 {
		t.Errorf("cold: %+v", cold)
	}
	if _, err := services.Create(ctx, model.Service{OrganizationID: org.ID, Kind: "utility", Name: "ХВС", Unit: "m3", CalcMethod: "fixed", ComponentIDs: []int64{}}); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "services_org_kind_name_key") {
		t.Errorf("duplicate name: %v", err)
	}

	// Тарифы: периоды не пересекаются, цена на сегодня попадает в список.
	add := func(svc int64, price float64, from model.Date, to *model.Date) (model.ServiceTariff, error) {
		return tariffs.Create(ctx, model.ServiceTariff{ServiceID: svc, Price: price, ValidFrom: from, ValidTo: to})
	}
	old, err := add(cold.ID, 40.5, date(2024, 1, 1), ptr(date(2025, 12, 31)))
	if err != nil {
		t.Fatal(err)
	}
	cur, err := add(cold.ID, 45.1234, date(2026, 1, 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := add(cold.ID, 1, date(2025, 6, 1), ptr(date(2026, 2, 1))); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "service_tariffs_no_overlap") {
		t.Errorf("overlap: %v", err)
	}
	if _, err := tariffs.Update(ctx, old.ID, model.ServiceTariff{ServiceID: cold.ID, Price: 40.5, ValidFrom: date(2024, 1, 1), ValidTo: ptr(date(2026, 1, 1))}); !errors.Is(err, ErrConflict) {
		t.Errorf("update overlap: %v", err)
	}
	if _, err := add(hot.ID, 99, date(2026, 1, 1), nil); err != nil {
		t.Fatal(err)
	}
	got, err := services.Get(ctx, cold.ID)
	if err != nil || got.CurrentPrice == nil || *got.CurrentPrice != 45.1234 {
		t.Fatalf("current price: %+v, %v", got.CurrentPrice, err)
	}
	list, err := tariffs.List(ctx, model.ServiceTariffFilter{ServiceID: &cold.ID}, 10, 0)
	if err != nil || len(list) != 2 || list[0].ID != cur.ID {
		t.Errorf("tariff list: %+v, %v", list, err)
	}

	// Составная услуга sum_prices: сумма цен слагаемых, своих тарифов нет.
	sum := make(model.Service{OrganizationID: org.ID, Kind: "utility", Name: "Водоотведение", Unit: "m3", CalcMethod: "sum_prices", ComponentIDs: []int64{cold.ID, hot.ID}})
	if len(sum.ComponentIDs) != 2 || sum.CurrentPrice == nil || *sum.CurrentPrice != 144.1234 {
		t.Errorf("sum service: %+v %v", sum.ComponentIDs, sum.CurrentPrice)
	}
	if _, err := add(sum.ID, 1, date(2026, 1, 1), nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("tariff for sum service: %v", err)
	}
	// Составная услуга sum_volumes: водоотведение = ХВС + ХВС для ГВС по объёму, а тариф на водоотведение свой.
	vol := make(model.Service{OrganizationID: org.ID, Kind: "utility", Name: "Водоотведение (объём)", Unit: "m3", CalcMethod: "sum_volumes", ComponentIDs: []int64{cold.ID, hot.ID}})
	if len(vol.ComponentIDs) != 2 || vol.CurrentPrice != nil {
		t.Errorf("sum_volumes service before tariff: %+v %v", vol.ComponentIDs, vol.CurrentPrice)
	}
	volTariff, err := add(vol.ID, 30.5, date(2026, 1, 1), nil)
	if err != nil {
		t.Fatalf("tariff for sum_volumes service: %v", err)
	}
	if got, err := services.Get(ctx, vol.ID); err != nil || got.CurrentPrice == nil || *got.CurrentPrice != 30.5 {
		t.Errorf("sum_volumes price: %+v, %v", got.CurrentPrice, err)
	}
	bad := func(name string, in model.Service) {
		t.Helper()
		in.Active = true
		in.SetDefaults()
		if _, err := services.Create(ctx, in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want invalid", name, err)
		}
	}
	bad("composite component", model.Service{OrganizationID: org.ID, Kind: "utility", Name: "Сумма сумм", Unit: "m3", CalcMethod: "sum_prices", ComponentIDs: []int64{sum.ID}})
	bad("other unit", model.Service{OrganizationID: org.ID, Kind: "utility", Name: "Сумма единиц", Unit: "m3", CalcMethod: "sum_prices", ComponentIDs: []int64{e2.ID}})
	otherSvc, err := services.Create(ctx, model.Service{OrganizationID: other.ID, Kind: "utility", Name: "ХВС", Unit: "m3", CalcMethod: "individual_meter", MeterResource: res("cold_water"), MeterTariff: tar(1), ComponentIDs: []int64{}, AccountPurpose: "utilities", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	bad("other organization", model.Service{OrganizationID: org.ID, Kind: "utility", Name: "Чужая сумма", Unit: "m3", CalcMethod: "sum_prices", ComponentIDs: []int64{otherSvc.ID}})
	// Услугу, входящую в состав, нельзя превратить в сумму, а услугу с тарифами — тоже.
	upd := cold
	upd.CalcMethod, upd.MeterResource, upd.MeterTariff, upd.ComponentIDs = "sum_prices", nil, nil, []int64{hot.ID}
	if _, err := services.Update(ctx, cold.ID, upd); !errors.Is(err, ErrInvalid) {
		t.Errorf("make component a sum: %v", err)
	}

	// Удаление: нельзя, пока есть тарифы или услуга входит в состав; после — можно.
	for _, id := range []int64{cold.ID, hot.ID} {
		if err := services.Delete(ctx, id); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrConflict) {
			t.Errorf("delete %d with tariffs: %v", id, err)
		}
	}
	if err := services.Delete(ctx, sum.ID); err != nil {
		t.Fatal(err)
	}
	if err := tariffs.Delete(ctx, volTariff.ID); err != nil {
		t.Fatal(err)
	}
	if err := services.Delete(ctx, vol.ID); err != nil {
		t.Fatal(err)
	}
	if err := tariffs.Delete(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	if err := tariffs.Delete(ctx, cur.ID); err != nil {
		t.Fatal(err)
	}
	if err := services.Delete(ctx, cold.ID); err != nil {
		t.Errorf("delete after tariffs removed: %v", err)
	}
	// Организацию с услугами удалить нельзя.
	if err := NewOrganizations(pool).Delete(ctx, org.ID); err == nil {
		t.Error("delete organization with services: want error")
	}
	// Восстановленный тариф не должен пересекаться с новым.
	if _, err := tariffs.Restore(ctx, cur.ID); err == nil {
		t.Error("restore tariff of a deleted service: want error")
	}
}
