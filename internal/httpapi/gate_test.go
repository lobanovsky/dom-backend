package httpapi

import (
	"context"
	"dom-backend/internal/auth"
	"dom-backend/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeGate struct {
	person int64
	user   model.User
	in     model.GateInput
}

func (f *fakeGate) Properties(_ context.Context, p int64) ([]model.GateProperty, error) {
	f.person = p
	return []model.GateProperty{}, nil
}
func (f *fakeGate) List(_ context.Context, p int64, _ model.GateFilter, _, _ int) ([]model.GateRequest, error) {
	f.person = p
	return []model.GateRequest{}, nil
}
func (f *fakeGate) Create(_ context.Context, p int64, in model.GateInput) (model.GateRequest, error) {
	f.person = p
	f.in = in
	return model.GateRequest{ID: 1, PersonID: p}, nil
}
func (f *fakeGate) Act(_ context.Context, p int64, u model.User, _ int64, _, _ string) (model.GateRequest, error) {
	f.person = p
	f.user = u
	return model.GateRequest{}, nil
}
func (f *fakeGate) Events(_ context.Context, p, _ int64) ([]model.GateEvent, error) {
	f.person = p
	return []model.GateEvent{}, nil
}
func TestGateGuardAuthorization(t *testing.T) {
	f := newFakeUsers()
	f.add("guard", "guardpass1", model.RoleGuard)
	f.add("viewer", "viewerpass1", model.RoleViewer)
	f.add("admin", "adminpass1", model.RoleAdmin)
	gate := &fakeGate{}
	h := NewRouter(Deps{Auth: auth.New(f, "test-secret", time.Hour), Users: f, Gate: gate, Organizations: fakeOrganizations{}})
	guard := loginAs(t, h, "guard", "guardpass1")
	viewer := loginAs(t, h, "viewer", "viewerpass1")
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/api/v1/gate-access", 200}, {"GET", "/api/v1/gate-access/1/events", 200},
		{"POST", "/api/v1/gate-access/1/decision", 200}, {"GET", "/api/v1/auth/me", 200},
		{"GET", "/api/v1/organizations", 403}, {"GET", "/api/v1/persons", 403}, {"GET", "/api/v1/users", 403},
		{"GET", "/api/v1/resident-access", 403}, {"GET", "/api/v1/accounts", 403}, {"GET", "/api/v1/meter-readings", 403},
		{"POST", "/api/v1/gate-access", 403}, {"DELETE", "/api/v1/gate-access/1", 403},
		{"POST", "/api/v1/gate-access/1/decision/extra", 403}, {"POST", "/api/v1/users", 403},
	} {
		rec := do(h, tc.method, tc.path, `{"action":"approve","note":""}`, guard)
		if rec.Code != tc.status {
			t.Errorf("guard %s %s: %d want %d (%s)", tc.method, tc.path, rec.Code, tc.status, rec.Body.String())
		}
	}
	if gate.user.Role != model.RoleGuard {
		t.Fatal("decision must use authenticated guard")
	}
	for _, tc := range []struct{ method, path string }{{"GET", "/api/v1/gate-access"}, {"POST", "/api/v1/gate-access/1/decision"}, {"GET", "/api/v1/gate-access/1/events"}} {
		if rec := do(h, tc.method, tc.path, `{}`, viewer); rec.Code != 403 {
			t.Fatalf("viewer gate access: %d", rec.Code)
		}
	}
	if rec := do(h, "GET", "/api/v1/gate-access", ""); rec.Code != 401 {
		t.Fatalf("anonymous: %d", rec.Code)
	}
}
func TestGateResidentIdentity(t *testing.T) {
	f := &fakeGate{}
	mux := http.NewServeMux()
	gateHandlers{f}.registerResident(mux)
	request := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/resident/gate-access", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), residentKey{}, model.ResidentAccount{PersonID: 17}))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	body := `{"premises_id":4,"zone":"courtyard","phone":"8 999 123 45 67","recipient_name":"Анна","relation":"guest"}`
	if w := request(body); w.Code != 201 || f.person != 17 || f.in.Phone != "79991234567" {
		t.Fatalf("resident identity: %d %+v", w.Code, f)
	}
	if w := request(strings.TrimSuffix(body, "}") + `,"person_id":2}`); w.Code != 400 {
		t.Fatalf("forged actor field: %d", w.Code)
	}
	if w := request(strings.Replace(body, `"courtyard"`, `"all"`, 1)); w.Code != 422 {
		t.Fatalf("invalid zone: %d", w.Code)
	}
}

func (f *fakeGate) CreateBatch(_ context.Context, p int64, in model.GateBatchInput) ([]model.GateRequest, error) {
	f.person = p
	return []model.GateRequest{}, nil
}

func TestGateResidentBatch(t *testing.T) {
	f := &fakeGate{}
	mux := http.NewServeMux()
	gateHandlers{f}.registerResident(mux)
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/resident/gate-access/batch", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), residentKey{}, model.ResidentAccount{PersonID: 17}))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	body := `{"premises_ids":[1,2],"phone":"+7 926 793 63 63","recipient_name":"Анна","relation":"guest","no_personal_vehicle":true}`
	if w := call(body); w.Code != 201 || f.person != 17 {
		t.Fatalf("batch identity: %d %s", w.Code, w.Body.String())
	}
	if w := call(strings.Replace(body, `[1,2]`, `[]`, 1)); w.Code != 422 {
		t.Fatalf("empty selection: %d", w.Code)
	}
	if w := call(strings.TrimSuffix(body, "}") + `,"zone":"parking"}`); w.Code != 400 {
		t.Fatalf("client-supplied zone: %d", w.Code)
	}
}
