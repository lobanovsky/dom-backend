package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dom-backend/internal/auth"
	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

type residentFake struct {
	ResidentStore
	requests int
	hash     string
}

func (f *residentFake) Session(_ context.Context, token string) (model.ResidentAccount, error) {
	if token != "resident-valid" {
		return model.ResidentAccount{}, store.ErrNotFound
	}
	return model.ResidentAccount{ID: 42, PersonID: 7, Email: "a@example.test"}, nil
}
func (f *residentFake) Profile(_ context.Context, id int64) (model.Person, error) {
	return model.Person{FirstName: "Житель", LastName: "Тест"}, nil
}
func (f *residentFake) RequestToken(context.Context, string, string, string) error {
	f.requests++
	return nil
}
func (f *residentFake) Properties(_ context.Context, id int64) ([]model.ResidentProperty, error) {
	if id != 7 {
		panic("wrong person")
	}
	return []model.ResidentProperty{}, nil
}
func TestResidentHTTPIsolation(t *testing.T) {
	fake := &residentFake{}
	handler := NewRouter(Deps{Residents: fake, ResidentOptions: ResidentOptions{MailEnabled: true, BaseURL: "https://lk.example.test"}})
	cases := []struct {
		method, path, body, cookie, origin string
		want                               int
	}{
		{"GET", "/api/v1/resident/healthz", "", "", "", 200},
		{"GET", "/api/v1/resident/me", "", "", "", 401},
		{"GET", "/api/v1/resident/me", "", "dom_session=resident-valid", "", 401},
		{"GET", "/api/v1/resident/me", "", "dom_resident_session=resident-valid", "", 200},
		{"GET", "/api/v1/resident/properties", "", "dom_resident_session=resident-valid", "", 200},
		{"GET", "/api/v1/persons", "", "dom_resident_session=resident-valid", "", 401},
		{"GET", "/api/v1/users", "", "dom_resident_session=resident-valid", "", 401},
		{"PUT", "/api/v1/resident-access/7", `{"email":"a@example.test","enabled":true}`, "dom_resident_session=resident-valid", "", 401},
		{"POST", "/api/v1/resident/auth/register", `{"email":"a@example.test"}`, "", "https://evil.test", 403},
		{"POST", "/api/v1/resident/auth/register", `{"email":"a@example.test"}`, "", "", 200},
		{"POST", "/api/v1/resident/auth/register", `{"email":"unknown@example.test"}`, "", "", 200},
		{"POST", "/api/v1/resident/auth/register", `{"email":"bad"}`, "", "", 422},
		{"POST", "/api/v1/resident/meters/1/readings", `{"period":"2026-11-01","t1":0,"person_id":999}`, "dom_resident_session=resident-valid", "", 400},
		{"POST", "/api/v1/resident/meters/1/readings", `{"period":"2026-11-01"}`, "dom_resident_session=resident-valid", "", 422},
	}
	for _, c := range cases {
		t.Run(c.method+c.path+c.cookie+c.origin+c.body, func(t *testing.T) {
			r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
			if c.cookie != "" {
				r.Header.Set("Cookie", c.cookie)
			}
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != c.want {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
		})
	}
	if fake.requests != 2 {
		t.Fatalf("token requests: %d", fake.requests)
	}
}
func TestUnknownRoleDenied(t *testing.T) {
	if allowed(model.User{Role: "resident"}, http.MethodGet, "/api/v1/persons") {
		t.Fatal("unknown role can read administrative data")
	}
}
func TestResidentEmailAndLimiter(t *testing.T) {
	if email, ok := residentEmail(" A@Example.Test "); !ok || email != "a@example.test" {
		t.Fatal(email, ok)
	}
	for _, input := range []string{"Name <a@example.test>", "a@example.test\r\nBcc: b@example.test", ""} {
		if _, ok := residentEmail(input); ok {
			t.Fatal("invalid address accepted")
		}
	}
	h := &residentHandlers{limits: map[string]residentLimit{}}
	for i := 0; i < 3; i++ {
		if h.limited("x", 3) {
			t.Fatal("early limit")
		}
	}
	if !h.limited("x", 3) {
		t.Fatal("missing limit")
	}
}

func (f *residentFake) ByEmail(_ context.Context, email string) (model.ResidentAccount, error) {
	if email != "a@example.test" {
		return model.ResidentAccount{}, store.ErrNotFound
	}
	return model.ResidentAccount{ID: 42, PersonID: 7, Email: email, PasswordHash: f.hash}, nil
}
func (f *residentFake) NewSession(_ context.Context, id int64, hash string) (string, error) {
	if id != 42 || hash != f.hash {
		return "", store.ErrNotFound
	}
	return "resident-valid", nil
}
func TestResidentLoginCookie(t *testing.T) {
	hash, e := auth.HashPassword("valid-password")
	if e != nil {
		t.Fatal(e)
	}
	handler := NewRouter(Deps{Residents: &residentFake{hash: hash}})
	for _, input := range []struct {
		email, password string
		want            int
	}{{"a@example.test", "valid-password", 200}, {"a@example.test", "wrong", 401}, {"unknown@example.test", "valid-password", 401}} {
		body := `{"email":"` + input.email + `","password":"` + input.password + `"}`
		r := httptest.NewRequest("POST", "https://lk.example.test/api/v1/resident/auth/login", strings.NewReader(body))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != input.want {
			t.Fatalf("login %d %s", w.Code, w.Body.String())
		}
		if input.want == 200 {
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatal("missing cookie")
			}
			c := cookies[0]
			if c.Name != residentCookie || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/api/v1/resident" {
				t.Fatalf("cookie attributes: %+v", c)
			}
		}
	}
}

func TestResidentStatusBatchValidation(t *testing.T) {
	h := &residentHandlers{}
	for _, q := range []string{"", "?person_ids=", "?person_ids=-1", "?person_ids=abc", "?person_ids=1&other=2", "?person_ids=1&person_ids=2", "?person_ids=" + strings.Repeat("1,", 200) + "1"} {
		r := httptest.NewRequest("GET", "/api/v1/resident-access"+q, nil)
		r = r.WithContext(context.WithValue(r.Context(), userCtxKey{}, model.User{ID: 1, Role: model.RoleAdmin}))
		w := httptest.NewRecorder()
		h.listAccess(w, r)
		if w.Code != 400 {
			t.Fatalf("%s returned %d", q, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/resident-access?person_ids=1", nil)
	r = r.WithContext(context.WithValue(r.Context(), userCtxKey{}, model.User{ID: 1, Role: model.RoleViewer}))
	w := httptest.NewRecorder()
	h.listAccess(w, r)
	if w.Code != 403 {
		t.Fatal("viewer can read resident statuses")
	}
}

func (f *residentFake) ListAccess(_ context.Context, ids []int64) ([]model.ResidentAccess, error) {
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		panic("batch ids were not deduplicated")
	}
	return []model.ResidentAccess{{PersonID: 1, Configured: true, Enabled: true, Eligible: true}}, nil
}
func TestResidentStatusBatch(t *testing.T) {
	h := &residentHandlers{store: &residentFake{}}
	r := httptest.NewRequest("GET", "/api/v1/resident-access?person_ids=1,2,1", nil)
	r = r.WithContext(context.WithValue(r.Context(), userCtxKey{}, model.User{ID: 1, Role: model.RoleAdmin}))
	w := httptest.NewRecorder()
	h.listAccess(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"person_id":1`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
