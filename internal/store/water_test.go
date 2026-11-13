package store

import (
	"context"
	"dom-backend/internal/model"
	"errors"
	"testing"
	"time"
)

func TestWaterContractsAndCalculation(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "water-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "water-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	meters, readings := NewMeters(pool), NewMeterReadings(pool)
	makeMeter := func(serial string, installed model.Date, removed *model.Date, initial float64) model.Meter {
		m, err := meters.Create(ctx, model.Meter{BuildingID: &b.ID, Resource: "cold_water", Tariffs: 1, Coefficient: 1, SerialNumber: serial, InstalledAt: installed, RemovedAt: removed, InitialT1: initial})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "meters", m.ID)
		return m
	}
	old := makeMeter("water-old", date(2025, 1, 1), ptr(date(2026, 8, 28)), 100)
	replacement := makeMeter("water-new", date(2026, 8, 29), nil, 0)
	add := func(m model.Meter, d model.Date, v float64) {
		r, err := readings.Create(ctx, model.MeterReading{MeterID: m.ID, ReadingDate: d, Period: d.FirstOfMonth(), T1: v, Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "meter_readings", r.ID)
	}
	add(old, date(2026, 7, 31), 1000)
	add(old, date(2026, 8, 28), 1010)
	add(replacement, date(2026, 8, 31), 5)
	add(replacement, date(2026, 9, 30), 15)
	add(replacement, date(2026, 10, 31), 35)
	add(replacement, date(2026, 12, 31), 60)
	tariff := func(name, unit string, price float64, from model.Date, to *model.Date) model.Tariff {
		v, err := NewTariffs(pool).Create(ctx, model.Tariff{OrganizationID: org.ID, Name: name, Unit: unit})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "tariffs", v.ID)
		p, err := NewTariffPrices(pool).Create(ctx, model.TariffPrice{TariffID: v.ID, Price: price, ValidFrom: from, ValidTo: to})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "tariff_prices", p.ID)
		return v
	}
	water := tariff("water-price", "m3", 2, date(2026, 1, 1), ptr(date(2026, 9, 30)))
	sewer := tariff("sewer-price", "m3", 3, date(2026, 1, 1), nil)
	p, err := NewTariffPrices(pool).Create(ctx, model.TariffPrice{TariffID: water.ID, Price: 4, ValidFrom: date(2026, 10, 1), ValidTo: ptr(date(2026, 11, 30))})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "tariff_prices", p.ID)
	s := NewWater(pool)
	in := model.WaterContractUpdate{Name: "water-contract-test", MeterIDs: []int64{old.ID, replacement.ID}, WaterTariffID: water.ID, SewerTariffID: sewer.ID}
	if err := s.CreateWaterContract(ctx, in); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM water_contracts WHERE name=$1`, in.Name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "water_contracts", id)
	list, err := s.ListWaterContracts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || len(list[0].MeterIDs) != 2 {
		t.Fatalf("list: %+v", list)
	}
	for _, tc := range []struct {
		month                int
		volume, water, sewer float64
		complete             bool
		meters               int
	}{
		{7, 0, 0, 0, false, 1},   // Первое архивное показание не считается от нуля.
		{8, 15, 30, 45, true, 2}, // При замене складывается расход обоих приборов.
		{9, 10, 20, 30, true, 1},
		{10, 20, 80, 60, true, 1}, // Новая цена берётся из истории справочника.
		{11, 0, 0, 0, false, 1},
		{12, 25, 0, 75, false, 1}, // Разность с октябрём учтена; расчёт неполный только из-за отсутствия цены воды.
	} {
		r, err := s.CalculateWater(ctx, date(2026, time.Month(tc.month), 1))
		if err != nil {
			t.Fatal(err)
		}
		c := r.Contracts[0]
		if c.Volume != tc.volume || c.WaterAmount != tc.water || c.SewerAmount != tc.sewer || c.Total != tc.water+tc.sewer || c.Complete != tc.complete || len(c.Meters) != tc.meters {
			t.Fatalf("month %d: %+v", tc.month, c)
		}
	}
	// Пропуск месяца не блокирует расчёт; используются цена выбранного месяца и даты исходных замеров.
	gapMeter := makeMeter("water-gap", date(2025, 1, 1), nil, 0)
	add(gapMeter, date(2025, 12, 2), 200612)
	add(gapMeter, date(2026, 2, 28), 205641)
	gapSettings := in
	gapSettings.MeterIDs = []int64{gapMeter.ID}
	if err := s.UpdateWaterContract(ctx, id, gapSettings); err != nil {
		t.Fatal(err)
	}
	gapResult, err := s.CalculateWater(ctx, date(2026, 2, 1))
	if err != nil {
		t.Fatal(err)
	}
	gap := gapResult.Contracts[0].Meters[0]
	if !gapResult.Complete || gapResult.Total != 25145 || gap.Volume == nil || *gap.Volume != 5029 || !gap.ReadingGap || gap.PreviousDate == nil || gap.PreviousDate.Format("2006-01-02") != "2025-12-02" || gap.CurrentDate == nil || gap.CurrentDate.Format("2006-01-02") != "2026-02-28" {
		t.Fatalf("gap calculation: %+v %+v", gapResult, gap)
	}
	noReading, err := s.CalculateWater(ctx, date(2026, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if noReading.Complete || noReading.Total != 0 || noReading.Contracts[0].Meters[0].ReadingGap {
		t.Fatalf("missing current: %+v", noReading)
	}
	if err := s.UpdateWaterContract(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	normal, err := s.CalculateWater(ctx, date(2026, 9, 1))
	if err != nil {
		t.Fatal(err)
	}
	if normal.Contracts[0].Meters[0].ReadingGap {
		t.Fatal("consecutive readings must not warn")
	}
	// Первый расчёт после установки в конце предыдущего месяца — от начального значения.
	late := makeMeter("water-late-install", date(2026, 8, 29), nil, 7)
	add(late, date(2026, 9, 30), 17)
	saved := in
	in.MeterIDs = append(in.MeterIDs, late.ID)
	if err := s.UpdateWaterContract(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	first, err := s.CalculateWater(ctx, date(2026, 9, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Complete || first.Total != 100 {
		t.Fatalf("first billing after installation: %+v", first)
	}
	in = saved
	if err := s.UpdateWaterContract(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	// Цена стоков остаётся видимой, даже если для воды цены нет.
	add(replacement, date(2027, 1, 31), 70)
	r, err := s.CalculateWater(ctx, date(2027, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if r.Complete || r.WaterAmount != 0 || r.SewerAmount != 30 || r.Contracts[0].Meters[0].Status != "no_tariff" {
		t.Fatalf("missing tariff: %+v", r)
	}
	duplicate := in
	duplicate.Name = "water-duplicate"
	if err := s.CreateWaterContract(ctx, duplicate); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate meter: %v", err)
	}
	other, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "water-other-org"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", other.ID)
	foreign, err := NewTariffs(pool).Create(ctx, model.Tariff{OrganizationID: other.ID, Name: "water-foreign", Unit: "m3"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "tariffs", foreign.ID)
	foreignInput := in
	foreignInput.WaterTariffID = foreign.ID
	if err := s.UpdateWaterContract(ctx, id, foreignInput); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign tariff: %v", err)
	}
	foreignInput.SewerTariffID = foreign.ID
	if err := s.UpdateWaterContract(ctx, id, foreignInput); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign meter organization: %v", err)
	}
	wrong := tariff("wrong-unit", "kwh", 1, date(2026, 1, 1), nil)
	invalid := in
	invalid.WaterTariffID = wrong.ID
	if err := s.UpdateWaterContract(ctx, id, invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong unit: %v", err)
	}
	water.Unit = "kwh"
	if _, err := NewTariffs(pool).Update(ctx, water.ID, water); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tariff guard: %v", err)
	}
	if err := meters.Delete(ctx, replacement.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("meter guard: %v", err)
	}
	in.Name = "water-renamed"
	if err := s.UpdateWaterContract(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveWaterContract(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateWaterContract(ctx, id, in); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archived edit: %v", err)
	}
	historical, err := s.CalculateWater(ctx, date(2026, 8, 1))
	if err != nil || len(historical.Contracts) != 1 || historical.Total != 75 {
		t.Fatalf("archive history: %+v %v", historical, err)
	}
	future, err := s.CalculateWater(ctx, date(2100, 1, 1))
	if err != nil || len(future.Contracts) != 0 || future.Complete {
		t.Fatalf("archive future: %+v %v", future, err)
	}
}
