package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"dom-backend/internal/model"
)

type fakeElectricity struct {
	period     model.Date
	contractID int64
	update     model.ElectricityContractUpdate
}

func (f *fakeElectricity) ListElectricityContracts(context.Context) ([]model.ElectricityContract, error) {
	return []model.ElectricityContract{{ID: 1, Name: "Дом", MeterIDs: []int64{}, Prices: []model.ElectricityContractPrice{}}}, nil
}
func (f *fakeElectricity) UpdateElectricityContract(_ context.Context, id int64, in model.ElectricityContractUpdate) error {
	f.contractID = id
	f.update = in
	return nil
}
func (f *fakeElectricity) CalculateElectricity(_ context.Context, p model.Date) (model.ElectricityCalculation, error) {
	f.period = p
	return model.ElectricityCalculation{Period: p.Format("2006-01"), Complete: true}, nil
}

func TestElectricityHandlers(t *testing.T) {
	f := &fakeElectricity{}
	mux := http.NewServeMux()
	electricityHandlers{f}.register(mux)
	for _, path := range []string{"/api/v1/electricity-contracts/calculation", "/api/v1/electricity-contracts/calculation?period=2026-13", "/api/v1/electricity-contracts/calculation?period=2026-11&x=1"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 422 {
			t.Fatalf("%s: status %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/electricity-contracts/calculation?period=2026-11", nil))
	if w.Code != 200 || f.period.Format("2006-01-02") != "2026-11-01" {
		t.Fatalf("calculation: status %d period %s", w.Code, f.period)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/electricity-contracts", nil))
	if w.Code != 200 {
		t.Fatalf("list: %d", w.Code)
	}
}

func (f *fakeElectricity) CreateElectricityContract(context.Context, model.ElectricityContractUpdate) error {
	return nil
}
func (f *fakeElectricity) ArchiveElectricityContract(context.Context, int64) error { return nil }
