package store

import (
	"testing"
)

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
