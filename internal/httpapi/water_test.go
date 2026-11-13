package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dom-backend/internal/model"
)

type fakeWater struct {
	period     model.Date
	contractID int64
	update     model.WaterContractUpdate
}

func (f *fakeWater) ListWaterContracts(context.Context) ([]model.WaterContract, error) {
	return []model.WaterContract{{ID: 1, Name: "Дом", MeterIDs: []int64{}}}, nil
}
func (f *fakeWater) UpdateWaterContract(_ context.Context, id int64, in model.WaterContractUpdate) error {
	f.contractID = id
	f.update = in
	return nil
}
func (f *fakeWater) CalculateWater(_ context.Context, p model.Date) (model.WaterCalculation, error) {
	f.period = p
	return model.WaterCalculation{Period: p.Format("2006-01"), Complete: true}, nil
}

func TestWaterHandlers(t *testing.T) {
	f := &fakeWater{}
	mux := http.NewServeMux()
	waterHandlers{f}.register(mux)
	for _, path := range []string{"/api/v1/water-contracts/calculation", "/api/v1/water-contracts/calculation?period=2026-13", "/api/v1/water-contracts/calculation?period=2026-11&x=1"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 422 {
			t.Fatalf("%s: status %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/water-contracts/calculation?period=2026-11", nil))
	if w.Code != 200 || f.period.Format("2006-01-02") != "2026-11-01" {
		t.Fatalf("calculation: status %d period %s", w.Code, f.period)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/water-contracts", nil))
	if w.Code != 200 {
		t.Fatalf("list: %d", w.Code)
	}
}

func (f *fakeWater) CreateWaterContract(context.Context, model.WaterContractUpdate) error {
	return nil
}
func (f *fakeWater) ArchiveWaterContract(context.Context, int64) error { return nil }

func TestWaterWriteValidation(t *testing.T) {
	f := &fakeWater{}
	mux := http.NewServeMux()
	waterHandlers{f}.register(mux)
	for _, body := range []string{`{}`, `{"name":"Дом","water_tariff_id":1,"sewer_tariff_id":2,"meter_ids":[3,3]}`, `{"name":"Дом","water_tariff_id":1,"sewer_tariff_id":2,"meter_ids":[3],"prices":[]}`} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("PUT", "/api/v1/water-contracts/1", strings.NewReader(body)))
		if w.Code < 400 || f.contractID != 0 {
			t.Fatalf("invalid body accepted: %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("PUT", "/api/v1/water-contracts/7", strings.NewReader(`{"name":"Дом","water_tariff_id":1,"sewer_tariff_id":2,"meter_ids":[3]}`)))
	if w.Code != 200 || f.contractID != 7 || f.update.WaterTariffID != 1 || f.update.SewerTariffID != 2 {
		t.Fatalf("update: %d %+v", w.Code, f)
	}
}
