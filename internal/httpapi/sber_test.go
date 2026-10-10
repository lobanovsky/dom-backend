package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dom-backend/internal/model"
	"dom-backend/internal/sbersync"
)

type fakeSberStore struct {
	saved   *model.SberTokens
	updated *time.Time
	runs    []model.SberRun
}

func (f *fakeSberStore) Tokens(context.Context) (model.SberTokens, error) {
	return model.SberTokens{}, nil
}
func (f *fakeSberStore) SaveTokens(_ context.Context, t model.SberTokens) error {
	f.saved = &t
	return nil
}
func (f *fakeSberStore) TokensUpdatedAt(context.Context) (*time.Time, error) { return f.updated, nil }
func (f *fakeSberStore) Runs(context.Context, int) ([]model.SberRun, error)  { return f.runs, nil }

type fakeSyncer struct {
	from, to time.Time
	err      error
}

func (f *fakeSyncer) Start(_ context.Context, _ string, from, to time.Time) (int64, error) {
	f.from, f.to = from, to
	return 5, f.err
}

func sberDo(h sberHandlers, method, path, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestSberSync(t *testing.T) {
	syncer := &fakeSyncer{}
	h := sberHandlers{&fakeSberStore{}, syncer, SberInfo{Configured: true, Days: 3, Ctx: context.Background()}}

	rec := sberDo(h, "POST", "/api/v1/sber/sync", `{"date_from":"2026-01-01","date_to":"2026-01-10"}`)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"run_id":5`) {
		t.Fatalf("sync: %d %s", rec.Code, rec.Body)
	}
	if syncer.from.Format("2006-01-02") != "2026-01-01" || syncer.to.Format("2006-01-02") != "2026-01-10" {
		t.Errorf("period = %v..%v", syncer.from, syncer.to)
	}

	// без тела — последние SberInfo.Days дней, заканчивая сегодня
	if rec := sberDo(h, "POST", "/api/v1/sber/sync", ``); rec.Code != http.StatusAccepted {
		t.Fatalf("default period: %d %s", rec.Code, rec.Body)
	}
	if days := int(syncer.to.Sub(syncer.from).Hours()/24) + 1; days != 3 {
		t.Errorf("default period = %d days, want 3", days)
	}

	for body, want := range map[string]int{
		`{"date_from":"2026-02-01","date_to":"2026-01-01"}`: 422,
		`{"date_to":"2999-01-01"}`:                          422,
		`{"date_from":"2020-01-01","date_to":"2026-01-01"}`: 422, // длиннее 400 дней
		`{"unknown":1}`: 400,
	} {
		if rec := sberDo(h, "POST", "/api/v1/sber/sync", body); rec.Code != want {
			t.Errorf("%s: %d %s, want %d", body, rec.Code, rec.Body, want)
		}
	}

	syncer.err = sbersync.ErrBusy
	if rec := sberDo(h, "POST", "/api/v1/sber/sync", ``); rec.Code != http.StatusConflict {
		t.Errorf("busy: %d", rec.Code)
	}
	syncer.err = sbersync.ErrNotConfigured
	if rec := sberDo(h, "POST", "/api/v1/sber/sync", ``); rec.Code != http.StatusConflict {
		t.Errorf("no tokens: %d", rec.Code)
	}

	off := sberHandlers{&fakeSberStore{}, nil, SberInfo{}}
	if rec := sberDo(off, "POST", "/api/v1/sber/sync", ``); rec.Code != http.StatusConflict {
		t.Errorf("disabled: %d", rec.Code)
	}
}

func TestSberTokensAndStatus(t *testing.T) {
	st := &fakeSberStore{runs: []model.SberRun{{ID: 2}}}
	h := sberHandlers{st, &fakeSyncer{}, SberInfo{Configured: true, Interval: time.Hour}}

	if rec := sberDo(h, "PUT", "/api/v1/sber/tokens", `{"refresh_token":"  "}`); rec.Code != 422 {
		t.Errorf("empty token: %d", rec.Code)
	}
	if rec := sberDo(h, "PUT", "/api/v1/sber/tokens", `{"refresh_token":" abc "}`); rec.Code != 204 || st.saved == nil || st.saved.RefreshToken != "abc" || !st.saved.AccessExpiresAt.IsZero() {
		t.Errorf("save: %d %+v", rec.Code, st.saved)
	}

	rec := sberDo(h, "GET", "/api/v1/sber/status", ``)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `"configured":true`) || !strings.Contains(body, `"tokens_set":false`) ||
		!strings.Contains(body, `"running":true`) || !strings.Contains(body, `"schedule_interval":"1h0m0s"`) {
		t.Errorf("status: %d %s", rec.Code, body)
	}
	if strings.Contains(body, "abc") {
		t.Error("status must not expose tokens")
	}
}
