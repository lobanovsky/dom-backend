package store

import (
	"context"
	"dom-backend/internal/model"
	"errors"
	"testing"
	"time"
)

func TestElectricityEmptyContractQueries(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO electricity_contracts(name) VALUES('electricity-empty-query-test') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM electricity_contracts WHERE id=$1`, id) })
	s := NewElectricity(pool)
	contracts, err := s.ListElectricityContracts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range contracts {
		if c.ID == id {
			found = true
			if c.MeterIDs == nil || c.Prices == nil {
				t.Fatal("empty collections must be arrays")
			}
		}
	}
	if !found {
		t.Fatal("contract missing")
	}
	result, err := s.CalculateElectricity(ctx, date(2026, 11, 1))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range result.Contracts {
		if c.ID == id {
			if c.Complete || len(c.Meters) != 0 {
				t.Fatalf("unexpected empty contract: %+v", c)
			}
			return
		}
	}
	t.Fatal("calculation contract missing")
}

func float(v float64) *float64 { return &v }

func TestCalculateElectricityMeter(t *testing.T) {
	t.Run("three zones with coefficient-adjusted consumption", func(t *testing.T) {
		row := electricityCalcRow{Tariffs: 3, D1: float(3600), D2: float(4480), D3: float(2720), P1: float(.1), P2: float(.2), P3: float(.3)}
		got := calculateElectricityMeter(row)
		if got.Status != "ok" || got.Amount != 2072 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("single tariff", func(t *testing.T) {
		got := calculateElectricityMeter(electricityCalcRow{Tariffs: 1, D1: float(201), P1: float(6.12)})
		if got.Amount != 1230.12 || got.Status != "ok" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("missing reading is incomplete", func(t *testing.T) {
		got := calculateElectricityMeter(electricityCalcRow{Tariffs: 1, P1: float(1)})
		if got.Status != "no_reading" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("missing zone price is incomplete", func(t *testing.T) {
		got := calculateElectricityMeter(electricityCalcRow{Tariffs: 2, D1: float(10), D2: float(5), P1: float(1)})
		if got.Status != "no_tariff" {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestElectricityContractManagement(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewElectricity(pool)
	name := "electricity-management-test"
	if err := s.CreateElectricityContract(ctx, model.ElectricityContractUpdate{Name: &name}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM electricity_contracts WHERE name=$1`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM electricity_contracts WHERE id=$1`, id) })
	renamed := name + "-renamed"
	number := "123"
	if err := s.UpdateElectricityContract(ctx, id, model.ElectricityContractUpdate{Name: &renamed, Number: &number}); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveElectricityContract(ctx, id); err != nil {
		t.Fatal(err)
	}
	contracts, err := s.ListElectricityContracts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range contracts {
		if c.ID == id {
			if c.Name != renamed || c.Number == nil || *c.Number != number || c.ArchivedAt == nil {
				t.Fatalf("unexpected contract: %+v", c)
			}
			return
		}
	}
	t.Fatal("archived contract missing")
}

// Проверяет настоящий SQL расчёта: зоны, коэффициенты, историю цен и отсутствие подмены цены в пропуске.
func TestElectricityCatalogTariffs(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "electricity-catalog-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "electricity-catalog-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	meters, readings := NewMeters(pool), NewMeterReadings(pool)
	meter, err := meters.Create(ctx, model.Meter{BuildingID: &b.ID, Resource: "electricity", Tariffs: 3, Coefficient: 80, SerialNumber: "catalog-three-zone", InstalledAt: date(2026, 9, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "meters", meter.ID)
	for i := 0; i < 4; i++ {
		r, err := readings.Create(ctx, model.MeterReading{MeterID: meter.ID, ReadingDate: date(2026, time.Month(9+i), 25), Period: date(2026, time.Month(9+i), 1), T1: 100 + float64(i)*10, T2: float(200 + float64(i)*20), T3: float(300 + float64(i)*30), Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "meter_readings", r.ID)
	}
	catalog, prices := NewTariffs(pool), NewTariffPrices(pool)
	makeTariff := func(name, unit string) model.Tariff {
		v, err := catalog.Create(ctx, model.Tariff{OrganizationID: org.ID, Name: name, Unit: unit})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "tariffs", v.ID)
		return v
	}
	t1, t2, t3 := makeTariff("catalog T1", "kwh"), makeTariff("catalog T2", "kwh"), makeTariff("catalog T3", "kwh")
	addPrice := func(id int64, price float64, from model.Date, to *model.Date) {
		v, err := prices.Create(ctx, model.TariffPrice{TariffID: id, Price: price, ValidFrom: from, ValidTo: to})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "tariff_prices", v.ID)
	}
	addPrice(t1.ID, 1, date(2026, 10, 1), ptr(date(2026, 10, 31)))
	addPrice(t1.ID, 2, date(2026, 11, 1), nil)
	addPrice(t2.ID, 3, date(2026, 10, 1), nil)
	addPrice(t3.ID, 4, date(2026, 10, 1), ptr(date(2026, 11, 30)))
	wrong := makeTariff("catalog wrong unit", "m3")
	otherOrg, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "electricity-catalog-other"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", otherOrg.ID)
	foreign, err := catalog.Create(ctx, model.Tariff{OrganizationID: otherOrg.ID, Name: "catalog foreign org", Unit: "kwh"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "tariffs", foreign.ID)

	s := NewElectricity(pool)
	name := "catalog electricity contract"
	in := model.ElectricityContractUpdate{Name: &name, MeterIDs: []int64{meter.ID}, Prices: []model.ElectricityContractPrice{{ValidFrom: date(2026, 10, 1), T1: 99, T2: float(99), T3: float(99)}}}
	if err := s.CreateElectricityContract(ctx, in); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM electricity_contracts WHERE name=$1`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "electricity_contracts", id)
	in.Prices = nil
	in.TariffT1ID = &t1.ID
	in.TariffT2ID = &t2.ID
	in.TariffT3ID = &t3.ID
	if err := s.UpdateElectricityContract(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		month  int
		amount float64
		status string
	}{{10, 15200, "ok"}, {11, 16000, "ok"}, {12, 0, "no_tariff"}} {
		result, err := s.CalculateElectricity(ctx, date(2026, time.Month(tc.month), 1))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range result.Contracts {
			if c.ID == id {
				found = true
				if len(c.Meters) != 1 || c.Meters[0].Status != tc.status || c.Total != tc.amount || c.Complete != (tc.status == "ok") {
					t.Fatalf("month %d: %+v", tc.month, c)
				}
			}
		}
		if !found {
			t.Fatal("calculation contract missing")
		}
	}
	contracts, err := s.ListElectricityContracts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range contracts {
		if c.ID == id && (c.TariffT1ID == nil || *c.TariffT1ID != t1.ID || len(c.Prices) != 1) {
			t.Fatalf("catalog references or legacy prices lost: %+v", c)
		}
	}
	in.TariffT2ID = nil
	if err := s.UpdateElectricityContract(ctx, id, in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing zone tariff: %v", err)
	}
	in.TariffT2ID = &t2.ID
	in.TariffT1ID = &wrong.ID
	if err := s.UpdateElectricityContract(ctx, id, in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong tariff unit: %v", err)
	}

	in.TariffT1ID = &foreign.ID
	if err := s.UpdateElectricityContract(ctx, id, in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign tariff organization: %v", err)
	}
	t1.Unit = "m3"
	if _, err := catalog.Update(ctx, t1.ID, t1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("changing referenced tariff unit: %v", err)
	}
	// Однотарифный договор использует только Т1.
	single, err := meters.Create(ctx, model.Meter{BuildingID: &b.ID, Resource: "electricity", Tariffs: 1, Coefficient: 1, SerialNumber: "catalog-single-zone", InstalledAt: date(2026, 10, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "meters", single.ID)
	reading, err := readings.Create(ctx, model.MeterReading{MeterID: single.ID, ReadingDate: date(2026, 10, 25), Period: date(2026, 10, 1), T1: 5, Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "meter_readings", reading.ID)
	singleName := "catalog single contract"
	if err := s.CreateElectricityContract(ctx, model.ElectricityContractUpdate{Name: &singleName, MeterIDs: []int64{single.ID}, TariffT1ID: &t1.ID}); err != nil {
		t.Fatal(err)
	}
	var singleID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM electricity_contracts WHERE name=$1`, singleName).Scan(&singleID); err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "electricity_contracts", singleID)
	result, err := s.CalculateElectricity(ctx, date(2026, 10, 1))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range result.Contracts {
		if c.ID == singleID {
			if !c.Complete || c.Total != 5 {
				t.Fatalf("single tariff: %+v", c)
			}
			return
		}
	}
	t.Fatal("single tariff contract missing")
}
