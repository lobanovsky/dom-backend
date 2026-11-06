package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dom-backend/internal/model"
)

type fakePersonsReport struct{ day time.Time }

func (f *fakePersonsReport) Build(_ context.Context, day time.Time) (model.PersonsReport, error) {
	f.day = day
	return model.PersonsReport{Day: day.Format("2006-01-02"), Persons: []model.PersonsReportPerson{
		{Name: "Иванов Иван", Items: []model.PersonsReportItem{{Label: "кв.001", ShareNum: 1, ShareDen: 1}}},
	}}, nil
}

// Маршрут /persons/properties-report не должен конфликтовать с /persons/{id}.
func TestPersonsReportRoute(t *testing.T) {
	fake := &fakePersonsReport{}
	mux := http.NewServeMux()
	personHandlers{}.register(mux)
	personsReportHandlers{fake}.register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/persons/properties-report?date=2026-05-01", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/pdf" || !strings.HasPrefix(rec.Body.String(), "%PDF-") {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := fake.day.Format("2006-01-02"); got != "2026-05-01" {
		t.Errorf("date = %s", got)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("Content-Disposition = %q", rec.Header().Get("Content-Disposition"))
	}

	bad := httptest.NewRecorder()
	mux.ServeHTTP(bad, httptest.NewRequest("GET", "/api/v1/persons/properties-report?date=yesterday", nil))
	if bad.Code != http.StatusBadRequest {
		t.Errorf("bad date: status %d", bad.Code)
	}
}
