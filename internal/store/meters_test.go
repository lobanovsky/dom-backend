package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dom-backend/internal/model"
)

func TestMetersAndReadings(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	meters, readings := NewMeters(pool), NewMeterReadings(pool)

	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "meter-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "meter-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	pr, err := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "1"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises", pr.ID)

	// Трёхтарифный электросчётчик помещения, начальные показания не нулевые.
	elec, err := meters.Create(ctx, model.Meter{PremisesID: &pr.ID, Resource: "electricity", Tariffs: 3, SerialNumber: "meter-test-1", Coefficient: 2,
		InstalledAt: date(2026, 1, 1), InitialT1: 100, InitialT2: 50, InitialT3: 10, NextVerificationAt: ptr(date(2020, 1, 1))})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "meters", elec.ID)
	if !elec.Active || !elec.VerificationOverdue || elec.LocationBuildingID != b.ID || *elec.PremisesNumber != "1" || elec.LastReadingDate != nil {
		t.Errorf("computed fields: %+v", elec)
	}
	// Общедомовой счётчик воды.
	water, err := meters.Create(ctx, model.Meter{BuildingID: &b.ID, Resource: "cold_water", Tariffs: 1, SerialNumber: "meter-test-2", Coefficient: 1, InstalledAt: date(2026, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "meters", water.ID)
	if water.LocationBuildingID != b.ID || water.PremisesNumber != nil {
		t.Errorf("common meter: %+v", water)
	}
	if _, err := meters.Create(ctx, model.Meter{BuildingID: &b.ID, Resource: "cold_water", Tariffs: 1, SerialNumber: "meter-test-2", Coefficient: 1, InstalledAt: date(2026, 1, 1)}); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "meters_serial_number_key") {
		t.Errorf("duplicate serial: %v", err)
	}

	f := func(v float64) *float64 { return &v }
	add := func(day int, t1 float64, t2, t3 *float64) (model.MeterReading, error) {
		return readings.Create(ctx, model.MeterReading{MeterID: elec.ID, ReadingDate: date(2026, 2, day), T1: t1, T2: t2, T3: t3, Source: "manual"})
	}
	r1, err := add(1, 110, f(60), f(15))
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "meter_readings", r1.ID)
	// Расход первого показания — от начальных, с коэффициентом 2.
	if *r1.DeltaT1 != 20 || *r1.DeltaT2 != 20 || *r1.DeltaT3 != 10 {
		t.Errorf("first delta: %v %v %v", *r1.DeltaT1, *r1.DeltaT2, *r1.DeltaT3)
	}
	r3, err := add(20, 150, f(80), f(25))
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "meter_readings", r3.ID)
	if *r3.DeltaT1 != 80 {
		t.Errorf("delta between: %v", *r3.DeltaT1)
	}
	// Показание между существующими: расход считается от соседа слева, а у правого пересчитывается.
	r2, err := add(10, 130, f(70), f(20))
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "meter_readings", r2.ID)
	if got, _ := readings.Get(ctx, r3.ID); *got.DeltaT1 != 40 {
		t.Errorf("right neighbour delta: %v", *got.DeltaT1)
	}

	for name, tc := range map[string]struct {
		day    int
		t1     float64
		t2, t3 *float64
		want   string
	}{
		"less than previous": {15, 125, f(75), f(22), "t1: must not be less than previous"},
		"more than next":     {15, 160, f(75), f(22), "t1: must not be greater than next"},
		"t2 below previous":  {15, 140, f(65), f(22), "t2: must not be less than previous"},
		"below initial":      {0, 99, f(60), f(15), "must not be before"},
		"missing t3":         {15, 140, f(75), nil, "t3: is required"},
		"duplicate date":     {10, 131, f(71), f(21), "meter_readings_meter_date_key"},
	} {
		day := tc.day
		if day == 0 {
			day = 1 // дата до начальной проверяется отдельным вводом ниже
		}
		_, err := add(day, tc.t1, tc.t2, tc.t3)
		if name == "below initial" {
			_, err = readings.Create(ctx, model.MeterReading{MeterID: elec.ID, ReadingDate: date(2025, 12, 31), T1: 99, T2: f(60), T3: f(15), Source: "manual"})
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	// Водяной счётчик: T2 недопустим.
	if _, err := readings.Create(ctx, model.MeterReading{MeterID: water.ID, ReadingDate: date(2026, 2, 1), T1: 5, T2: f(1), Source: "manual"}); err == nil || !strings.Contains(err.Error(), "t2: must be empty") {
		t.Errorf("water with t2: %v", err)
	}

	// Правка и удаление с восстановлением.
	upd, err := readings.Update(ctx, r2.ID, model.MeterReading{MeterID: elec.ID, ReadingDate: date(2026, 2, 10), T1: 135, T2: f(70), T3: f(20), Source: "manual"})
	if err != nil || *upd.DeltaT1 != 50 {
		t.Errorf("update: %v %+v", err, upd)
	}
	if _, err := readings.Update(ctx, r2.ID, model.MeterReading{MeterID: elec.ID, ReadingDate: date(2026, 2, 10), T1: 151, T2: f(70), T3: f(20), Source: "manual"}); err == nil {
		t.Error("update above next reading: want error")
	}
	if err := readings.Delete(ctx, r2.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := readings.Get(ctx, r3.ID); *got.DeltaT1 != 80 {
		t.Errorf("delta after delete: %v", *got.DeltaT1)
	}
	if _, err := readings.Restore(ctx, r2.ID); err != nil {
		t.Errorf("restore: %v", err)
	}

	// Список: фильтры и последнее показание.
	list, err := readings.List(ctx, model.MeterReadingFilter{MeterID: &elec.ID, DateFrom: ptr(date(2026, 2, 5))}, 50, 0)
	if err != nil || len(list) != 2 || list[0].ID != r3.ID {
		t.Errorf("list: %v %+v", err, list)
	}
	got, err := meters.Get(ctx, elec.ID)
	if err != nil || got.LastReadingDate == nil || *got.LastT1 != 150 {
		t.Errorf("last reading: %v %+v", err, got)
	}
	ml, err := meters.List(ctx, model.MeterFilter{BuildingID: &b.ID}, 50, 0)
	if err != nil || len(ml) != 2 || ml[0].ID != water.ID {
		t.Errorf("list by building (common first): %v %+v", err, ml)
	}
	if ml, _ := meters.List(ctx, model.MeterFilter{Scope: ptr("individual"), BuildingID: &b.ID}, 50, 0); len(ml) != 1 || ml[0].ID != elec.ID {
		t.Errorf("individual scope: %+v", ml)
	}

	// Изменения счётчика, ломающие введённые показания, запрещены.
	in := asInput(elec)
	in.Tariffs = 2
	if _, err := meters.Update(ctx, elec.ID, in); err == nil || !strings.Contains(err.Error(), "tariffs") {
		t.Errorf("change tariffs: %v", err)
	}
	in = asInput(elec)
	in.RemovedAt = ptr(date(2026, 2, 5))
	if _, err := meters.Update(ctx, elec.ID, in); err == nil {
		t.Error("removed_at before last reading: want error")
	}
	in = asInput(elec)
	in.RemovedAt = ptr(date(2026, 3, 1))
	if upd, err := meters.Update(ctx, elec.ID, in); err != nil || upd.RemovedAt == nil {
		t.Errorf("valid update: %v", err)
	}
	// Показание после даты окончания не принимается.
	if _, err := readings.Create(ctx, model.MeterReading{MeterID: elec.ID, ReadingDate: date(2026, 3, 2), T1: 200, T2: f(100), T3: f(40), Source: "manual"}); err == nil {
		t.Error("reading after removal: want error")
	}

	// Каскадные правила удаления.
	if err := meters.Delete(ctx, elec.ID); err == nil || !strings.Contains(err.Error(), "has active readings") {
		t.Errorf("delete meter with readings: %v", err)
	}
	if err := NewPremisesStore(pool).Delete(ctx, pr.ID); err == nil || !strings.Contains(err.Error(), "has active meters") {
		t.Errorf("delete premises with meters: %v", err)
	}
	if err := NewBuildings(pool).Delete(ctx, b.ID); err == nil {
		t.Error("delete building with meters: want error")
	}
}

// asInput — счётчик из ответа API в виде тела запроса (PUT заменяет запись целиком).
func asInput(m model.Meter) model.Meter {
	m.Meta = model.Meta{}
	return m
}
