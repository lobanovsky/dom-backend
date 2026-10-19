package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dom-backend/internal/model"
)

func TestImportReadings(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	readings := NewMeterReadings(pool)

	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "meter-import-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "meter-import-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	for _, n := range []string{"1", "2"} {
		pr, err := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: n})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "premises", pr.ID)
	}

	v := func(x float64) *float64 { return &x }
	row := func(n int, premises, serial string, t ...float64) model.MeterReadingImportRow {
		r := model.MeterReadingImportRow{Row: n, Premises: premises, Serial: serial}
		for i := range t {
			r.T[i] = v(t[i])
		}
		return r
	}
	sep := date(2026, 9, 30)
	installed := ptr(date(2015, 1, 1))

	// Без даты ввода неизвестный счётчик — ошибка строки, ничего не записывается.
	_, err = readings.ImportReadings(ctx, b.ID, "cold_water", sep, sep.FirstOfMonth(), nil, []model.MeterReadingImportRow{row(2, "1", "mi-1", 10)})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "row 2") {
		t.Errorf("unknown meter: %v", err)
	}

	// Счётчики создаются, у квартиры 1 их два.
	res, err := readings.ImportReadings(ctx, b.ID, "cold_water", sep, sep.FirstOfMonth(), installed, []model.MeterReadingImportRow{
		row(2, "1", "mi-1", 10.5), row(3, "1", "mi-2", 20), row(4, "2", "mi-3", 30),
	})
	if err != nil || res.MetersCreated != 3 || res.Readings != 3 {
		t.Fatalf("import: %+v, %v", res, err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM meter_readings WHERE meter_id IN (SELECT id FROM meters WHERE serial_number LIKE 'mi-%')`)
		pool.Exec(ctx, `DELETE FROM meters WHERE serial_number LIKE 'mi-%'`)
	})
	list, err := NewMeters(pool).List(ctx, model.MeterFilter{BuildingID: &b.ID}, 10, 0)
	if err != nil || len(list) != 3 || list[0].LastT1 == nil || *list[0].LastT1 != 10.5 || list[0].InstalledAt != *installed {
		t.Fatalf("meters: %+v, %v", list, err)
	}

	// Повторная загрузка за тот же месяц — конфликт, число счётчиков не растёт.
	_, err = readings.ImportReadings(ctx, b.ID, "cold_water", sep, sep.FirstOfMonth(), installed, []model.MeterReadingImportRow{row(2, "1", "mi-1", 11)})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("same date: %v", err)
	}
	// Следующий месяц: показание меньше предыдущего и чужая квартира отклоняются.
	oct := date(2026, 10, 31)
	for _, c := range []struct {
		r    model.MeterReadingImportRow
		want string
	}{
		{row(5, "1", "mi-1", 9), "less than previous"},
		{row(6, "2", "mi-1", 12), "another premises"},
		{row(7, "99", "mi-1", 12), "not found"},
		{row(8, "1", "mi-1", 12, 5), "tariff"},
	} {
		if _, err := readings.ImportReadings(ctx, b.ID, "cold_water", oct, oct.FirstOfMonth(), nil, []model.MeterReadingImportRow{c.r}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: err = %v, want %q", c.r, err, c.want)
		}
	}
	res, err = readings.ImportReadings(ctx, b.ID, "cold_water", oct, oct.FirstOfMonth(), nil, []model.MeterReadingImportRow{row(2, "1", "mi-1", 12), row(3, "1", "mi-2", 25)})
	if err != nil || res.MetersCreated != 0 || res.Readings != 2 {
		t.Errorf("next month: %+v, %v", res, err)
	}
}
