package store

import (
	"context"
	"testing"
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
