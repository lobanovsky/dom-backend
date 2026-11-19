package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"dom-backend/internal/model"
)

type fakeMeterCatalogExport struct {
	filter model.MeterFilter
	limit  int
}

func (f *fakeMeterCatalogExport) List(_ context.Context, filter model.MeterFilter, limit, _ int) ([]model.Meter, error) {
	f.filter, f.limit = filter, limit
	return []model.Meter{{Resource: "cold_water", SerialNumber: "123", InstalledAt: model.Date{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, ManualReadingRequired: true}}, nil
}

func TestMeterCatalogExportUsesListFiltersAndReturnsXLSX(t *testing.T) {
	store := &fakeMeterCatalogExport{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/meters/export?scope=individual&manual_reading_required=true&limit=10&offset=20", nil)
	meterCatalogExportHandlers{store}.export(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if store.filter.Scope == nil || *store.filter.Scope != "individual" || store.filter.ManualReadingRequired == nil || !*store.filter.ManualReadingRequired {
		t.Fatalf("filter = %+v, want scope=individual and manual_reading_required=true", store.filter)
	}
	if store.limit != maxMeterCatalogExport+1 {
		t.Fatalf("limit = %d, want export cap + 1", store.limit)
	}
	if got := w.Header().Get("Content-Disposition"); got != `attachment; filename="meters.xlsx"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if _, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len())); err != nil {
		t.Fatalf("response is not a valid xlsx zip: %v", err)
	}
}
