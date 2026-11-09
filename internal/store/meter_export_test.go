package store

import (
	"context"
	"dom-backend/internal/model"
	"testing"
)

func TestMeterExportPeriod(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "export-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "export-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	p, err := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "001"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises", p.ID)
	m, err := NewMeters(pool).Create(ctx, model.Meter{PremisesID: &p.ID, Resource: "cold_water", Tariffs: 1, Coefficient: 1, SerialNumber: "export-0001", InstalledAt: date(2026, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "meters", m.ID)
	s := NewMeterReadings(pool)
	r, err := s.Create(ctx, model.MeterReading{MeterID: m.ID, ReadingDate: date(2026, 12, 1), Period: date(2026, 11, 1), T1: 12.345, Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "meter_readings", r.ID)
	rows, err := s.Export(ctx, date(2026, 11, 1))
	if err != nil || len(rows) != 1 || rows[0].SerialNumber != "export-0001" || rows[0].T1 != 12.345 {
		t.Fatalf("rows: %+v %v", rows, err)
	}
	rows, err = s.Export(ctx, date(2026, 12, 1))
	if err != nil || len(rows) != 0 {
		t.Fatalf("wrong period: %+v %v", rows, err)
	}
	if err = s.Delete(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = s.Export(ctx, date(2026, 11, 1))
	if err != nil || len(rows) != 0 {
		t.Fatalf("deleted reading exported: %+v %v", rows, err)
	}
}
