package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"dom-backend/internal/auth"
	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

type fakeOrganizations struct{ OrganizationStore }

func (fakeOrganizations) List(context.Context, model.OrganizationFilter, int, int) ([]model.Organization, error) {
	return []model.Organization{{Name: "ТСН"}}, nil
}

func (fakeOrganizations) Get(_ context.Context, id int64) (model.Organization, error) {
	if id == 404 {
		return model.Organization{}, &store.Error{Kind: store.ErrNotFound, Msg: "not found"}
	}
	return model.Organization{Name: "ТСН"}, nil
}

func (fakeOrganizations) Create(_ context.Context, in model.Organization) (model.Organization, error) {
	if in.Name == "dup" {
		return model.Organization{}, &store.Error{Kind: store.ErrConflict, Msg: "already exists"}
	}
	return in, nil
}

func (fakeOrganizations) Update(_ context.Context, _ int64, in model.Organization) (model.Organization, error) {
	return in, nil
}

func (fakeOrganizations) Delete(context.Context, int64) error { return nil }

func (fakeOrganizations) Restore(_ context.Context, id int64) (model.Organization, error) {
	if id == 404 {
		return model.Organization{}, &store.Error{Kind: store.ErrNotFound, Msg: "not found"}
	}
	if id == 422 {
		return model.Organization{}, &store.Error{Kind: store.ErrInvalid, Msg: "cannot save: building is deleted"}
	}
	return model.Organization{Name: "ТСН"}, nil
}

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return NewRouter(Deps{
		Auth:          auth.New("admin", string(h), "secret", time.Hour),
		Organizations: fakeOrganizations{},
	})
}

func do(h http.Handler, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func login(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	rec := do(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"pw"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestHealthzIsPublic(t *testing.T) {
	if rec := do(newTestRouter(t), "GET", "/healthz", ""); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAPIRequiresSession(t *testing.T) {
	h := newTestRouter(t)
	if rec := do(h, "GET", "/api/v1/organizations", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"bad"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d, want 401", rec.Code)
	}
}

func TestAuthMe(t *testing.T) {
	h := newTestRouter(t)
	if rec := do(h, "GET", "/api/v1/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without session: status = %d, want 401", rec.Code)
	}
	rec := do(h, "GET", "/api/v1/auth/me", "", login(t, h))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"username":"admin"`) {
		t.Fatalf("with session: status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestOrganizationStatusCodes(t *testing.T) {
	h := newTestRouter(t)
	c := login(t, h)
	cases := []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/api/v1/organizations", "", 200},
		{"GET", "/api/v1/organizations?kind=uk", "", 200},
		{"GET", "/api/v1/organizations?limit=0", "", 400},
		{"GET", "/api/v1/organizations?bogus=1", "", 400},
		{"GET", "/api/v1/organizations/1", "", 200},
		{"GET", "/api/v1/organizations/404", "", 404},
		{"GET", "/api/v1/organizations/abc", "", 400},
		{"POST", "/api/v1/organizations", `{"kind":"tsn","name":"ТСН"}`, 201},
		{"POST", "/api/v1/organizations", `{"kind":"tsn","name":"dup"}`, 409},
		{"POST", "/api/v1/organizations", `{"kind":"bad","name":"ТСН"}`, 422},
		{"POST", "/api/v1/organizations", `{"kind":"tsn","name":"ТСН","hack":1}`, 400},
		{"POST", "/api/v1/organizations", `[1]`, 400},
		{"PUT", "/api/v1/organizations/1", `{"kind":"uk","name":"УК"}`, 200},
		{"PUT", "/api/v1/organizations/1", `{"kind":"uk"}`, 422},
		{"DELETE", "/api/v1/organizations/1", "", 204},
		{"GET", "/api/v1/organizations?deleted=only", "", 200},
		{"GET", "/api/v1/organizations?deleted=all", "", 400},
		{"POST", "/api/v1/organizations/1/restore", "", 200},
		{"POST", "/api/v1/organizations/404/restore", "", 404},
		{"POST", "/api/v1/organizations/422/restore", "", 422},
		{"POST", "/api/v1/organizations/abc/restore", "", 400},
	}
	for _, tc := range cases {
		if rec := do(h, tc.method, tc.path, tc.body, c); rec.Code != tc.want {
			t.Errorf("%s %s: status = %d, want %d (%s)", tc.method, tc.path, rec.Code, tc.want, rec.Body)
		}
	}
}
