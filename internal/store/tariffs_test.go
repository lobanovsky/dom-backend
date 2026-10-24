package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dom-backend/internal/model"
)

func TestTariffCatalog(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	services, own, tariffs, prices := NewServices(pool), NewServiceTariffs(pool), NewTariffs(pool), NewTariffPrices(pool)

	orgs := NewOrganizations(pool)
	org, err := orgs.Create(ctx, model.Organization{Kind: "tsn", Name: "tariffs-test"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := orgs.Create(ctx, model.Organization{Kind: "tsn", Name: "tariffs-test-other"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, id := range []int64{org.ID, other.ID} {
			pool.Exec(ctx, `UPDATE services SET tariff_id = NULL WHERE organization_id = $1`, id)
			pool.Exec(ctx, `DELETE FROM tariff_prices WHERE tariff_id IN (SELECT id FROM tariffs WHERE organization_id = $1)`, id)
			pool.Exec(ctx, `DELETE FROM tariffs WHERE organization_id = $1`, id)
			pool.Exec(ctx, `DELETE FROM service_components WHERE service_id IN (SELECT id FROM services WHERE organization_id = $1)`, id)
			pool.Exec(ctx, `DELETE FROM service_tariffs WHERE service_id IN (SELECT id FROM services WHERE organization_id = $1)`, id)
			pool.Exec(ctx, `DELETE FROM services WHERE organization_id = $1`, id)
			pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, id)
		}
	})

	res := func(s string) *string { return &s }
	mk := func(name, unit string, tariffID *int64, orgID int64) (model.Service, error) {
		in := model.Service{OrganizationID: orgID, Kind: "utility", Name: name, Unit: unit, CalcMethod: "common_meter", MeterResource: res("electricity"), Active: true, TariffID: tariffID}
		in.SetDefaults()
		in.MeterTariff = new(int)
		*in.MeterTariff = 1
		return services.Create(ctx, in)
	}

	tar, err := tariffs.Create(ctx, model.Tariff{OrganizationID: org.ID, Name: "Электроэнергия Т1", Unit: "kwh"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tariffs.Create(ctx, model.Tariff{OrganizationID: org.ID, Name: "Электроэнергия Т1", Unit: "kwh"}); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "tariffs_org_name_key") {
		t.Errorf("duplicate name: %v", err)
	}
	add := func(price float64, from model.Date, to *model.Date) (model.TariffPrice, error) {
		return prices.Create(ctx, model.TariffPrice{TariffID: tar.ID, Price: price, ValidFrom: from, ValidTo: to})
	}
	first, err := add(5.5, date(2026, 1, 1), ptr(date(2026, 6, 30)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := add(6.1, date(2026, 7, 1), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := add(7, date(2026, 6, 1), ptr(date(2026, 8, 1))); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "tariff_prices_no_overlap") {
		t.Errorf("overlap: %v", err)
	}

	// Три услуги берут одну цену из тарифа; цена меняется в одном месте.
	a, err := mk("Т1 квартиры", "kwh", &tar.ID, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := mk("Т1 машиноместа", "kwh", &tar.ID, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.TariffName == nil || *a.TariffName != "Электроэнергия Т1" || a.CurrentPrice == nil || *a.CurrentPrice != 6.1 || *b.CurrentPrice != 6.1 {
		t.Errorf("service with tariff: %+v %v", a.TariffName, a.CurrentPrice)
	}
	got, _ := tariffs.Get(ctx, tar.ID)
	if got.ServicesCount != 2 || got.CurrentPrice == nil || *got.CurrentPrice != 6.1 || got.OrganizationName != "tariffs-test" {
		t.Errorf("tariff: %+v", got)
		if got.CurrentPriceFrom == nil || *got.CurrentPriceFrom != date(2026, 7, 1) || got.CurrentPriceTo != nil {
			t.Errorf("current price period: from %v to %v", got.CurrentPriceFrom, got.CurrentPriceTo)
		}
	}
	byTariff, err := services.List(ctx, model.ServiceFilter{TariffID: &tar.ID}, 10, 0)
	if err != nil || len(byTariff) != 2 {
		t.Errorf("services by tariff: %d, %v", len(byTariff), err)
	}

	// Составная услуга «сумма цен» берёт цены слагаемых из справочника.
	sum := model.Service{OrganizationID: org.ID, Kind: "utility", Name: "Сумма", Unit: "kwh", CalcMethod: "sum_prices", Active: true, ComponentIDs: []int64{a.ID, b.ID}}
	sum.SetDefaults()
	if s, err := services.Create(ctx, sum); err != nil || s.CurrentPrice == nil || *s.CurrentPrice != 12.2 {
		t.Errorf("sum prices: %+v %v", s.CurrentPrice, err)
	}

	// Ограничения: единица и организация, собственные цены, sum_prices.
	if _, err := mk("Т1 в м3", "m3", &tar.ID, org.ID); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("unit mismatch: %v", err)
	}
	if _, err := mk("Т1 чужая", "kwh", &tar.ID, other.ID); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("org mismatch: %v", err)
	}
	if _, err := own.Create(ctx, model.ServiceTariff{ServiceID: a.ID, Price: 1, ValidFrom: date(2026, 1, 1)}); err == nil || !strings.Contains(err.Error(), "catalog tariff") {
		t.Errorf("own tariff on linked service: %v", err)
	}
	plain, err := mk("Т1 свой", "kwh", nil, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := own.Create(ctx, model.ServiceTariff{ServiceID: plain.ID, Price: 4, ValidFrom: date(2026, 1, 1)}); err != nil {
		t.Fatal(err)
	}
	plain.TariffID = &tar.ID
	if _, err := services.Update(ctx, plain.ID, plain); err == nil || !strings.Contains(err.Error(), "own tariffs") {
		t.Errorf("link with own tariffs: %v", err)
	}
	tar.Unit = "m3"
	if _, err := tariffs.Update(ctx, tar.ID, tar); err == nil || !strings.Contains(err.Error(), "used by services") {
		t.Errorf("change unit of used tariff: %v", err)
	}
	if err := tariffs.Delete(ctx, tar.ID); err == nil || !strings.Contains(err.Error(), "prices") {
		t.Errorf("delete with prices: %v", err)
	}

	// Тариф с услугами не удалить, организацию с тарифами — тоже; цену можно удалить и вернуть.
	if err := prices.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := prices.Restore(ctx, first.ID); err != nil {
		t.Errorf("restore price: %v", err)
	}
	if err := orgs.Delete(ctx, org.ID); err == nil {
		t.Errorf("delete organization with tariffs")
	}
}
