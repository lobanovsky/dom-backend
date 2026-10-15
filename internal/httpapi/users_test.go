package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"dom-backend/internal/auth"
	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

// fakeUsers — пользователи в памяти: и источник для auth, и UserStore для /users.
type fakeUsers struct {
	mu    sync.Mutex
	users map[int64]*model.User
	next  int64

	events  []model.LoginEvent
	touched map[int64]int
}

func newFakeUsers() *fakeUsers { return &fakeUsers{users: map[int64]*model.User{}} }

func notFound() error { return &store.Error{Kind: store.ErrNotFound, Msg: "not found"} }

func (f *fakeUsers) add(name, password, role string) model.User {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	f.next++
	u := &model.User{ID: f.next, Username: name, Role: role, Active: true, PasswordHash: string(h)}
	f.users[u.ID] = u
	return *u
}

func (f *fakeUsers) ByUsername(_ context.Context, name string) (model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if strings.EqualFold(u.Username, name) {
			return *u, nil
		}
	}
	return model.User{}, notFound()
}

func (f *fakeUsers) ByID(_ context.Context, id int64) (model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[id]; ok {
		return *u, nil
	}
	return model.User{}, notFound()
}

func (f *fakeUsers) List(context.Context) ([]model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []model.User{}
	for _, u := range f.users {
		out = append(out, *u)
	}
	return out, nil
}

func (f *fakeUsers) Create(_ context.Context, name, hash, role string) (model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if strings.EqualFold(u.Username, name) {
			return model.User{}, &store.Error{Kind: store.ErrConflict, Msg: "already exists: users_username_key"}
		}
	}
	f.next++
	u := &model.User{ID: f.next, Username: name, Role: role, Active: true, PasswordHash: hash}
	f.users[u.ID] = u
	return *u, nil
}

func (f *fakeUsers) Update(_ context.Context, id int64, role string, active bool) (model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return model.User{}, notFound()
	}
	if u.IsAdmin() && u.Active && (role != model.RoleAdmin || !active) {
		others := 0
		for _, o := range f.users {
			if o.ID != id && o.IsAdmin() && o.Active {
				others++
			}
		}
		if others == 0 {
			return model.User{}, &store.Error{Kind: store.ErrConflict, Msg: "cannot remove the last admin"}
		}
	}
	if u.Active && !active {
		u.TokenVersion++
	}
	u.Role, u.Active = role, active
	return *u, nil
}

func (f *fakeUsers) SetPassword(_ context.Context, id int64, hash string) (model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return model.User{}, notFound()
	}
	u.PasswordHash = hash
	u.TokenVersion++
	return *u, nil
}

func (f *fakeUsers) RecordLogin(_ context.Context, e model.LoginEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e.ID = int64(len(f.events) + 1)
	f.events = append(f.events, e)
	return nil
}

func (f *fakeUsers) Touch(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.touched == nil {
		f.touched = map[int64]int{}
	}
	f.touched[id]++
	return nil
}

func (f *fakeUsers) Logins(_ context.Context, userID int64, limit, offset int) ([]model.LoginEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []model.LoginEvent{}
	for i := len(f.events) - 1; i >= 0; i-- {
		if e := f.events[i]; userID == 0 || (e.UserID != nil && *e.UserID == userID) {
			out = append(out, e)
		}
	}
	return out, nil
}

// testAuth — Auth с единственным администратором admin / пароль, чей bcrypt-хеш передан.
func testAuth(hash string) *auth.Auth {
	f := newFakeUsers()
	f.next++
	f.users[f.next] = &model.User{ID: f.next, Username: "admin", Role: model.RoleAdmin, Active: true, PasswordHash: hash}
	return auth.New(f, "secret", time.Hour)
}

// usersRouter — роутер с администратором admin/adminpw и наблюдателем viewer/viewerpw.
func usersRouter(t *testing.T) (http.Handler, *fakeUsers) {
	t.Helper()
	f := newFakeUsers()
	f.add("admin", "adminpw1", model.RoleAdmin)
	f.add("viewer", "viewerpw1", model.RoleViewer)
	return NewRouter(Deps{Auth: auth.New(f, "secret", time.Hour), Users: f, Organizations: fakeOrganizations{}}), f
}

func loginAs(t *testing.T, h http.Handler, user, password string) *http.Cookie {
	t.Helper()
	rec := do(h, "POST", "/api/v1/auth/login", `{"username":"`+user+`","password":"`+password+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: status = %d", user, rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestViewerIsReadOnly(t *testing.T) {
	h, _ := usersRouter(t)
	viewer := loginAs(t, h, "viewer", "viewerpw1")

	// Любое изменение запрещено: проверяем по одному маршруту каждого вида (запрет не зависит от сущности, он в middleware).
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v1/organizations"},
		{"PUT", "/api/v1/organizations/1"},
		{"DELETE", "/api/v1/organizations/1"},
		{"POST", "/api/v1/organizations/1/restore"},
		{"POST", "/api/v1/payment-registries/import"},
		{"POST", "/api/v1/bank-statements/import"},
		{"POST", "/api/v1/payment-assignments"},
		{"POST", "/api/v1/payment-assignments/preview"},
		{"POST", "/api/v1/payment-exports"},
		{"DELETE", "/api/v1/payment-exports/1"},
		{"POST", "/api/v1/sber/sync"},
		{"PUT", "/api/v1/sber/tokens"},
		{"POST", "/api/v1/payment-rules/reorder"},
		{"POST", "/api/v1/users"},
		{"PUT", "/api/v1/users/1"},
		{"GET", "/api/v1/users"},
	} {
		if rec := do(h, c.method, c.path, `{}`, viewer); rec.Code != http.StatusForbidden {
			t.Errorf("viewer %s %s: status = %d, want 403", c.method, c.path, rec.Code)
		}
	}

	// Чтение разрешено.
	if rec := do(h, "GET", "/api/v1/organizations", "", viewer); rec.Code != http.StatusOK {
		t.Errorf("viewer GET organizations: status = %d, want 200", rec.Code)
	}
	rec := do(h, "GET", "/api/v1/auth/me", "", viewer)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"role":"viewer"`) {
		t.Errorf("me = %d %s", rec.Code, rec.Body.String())
	}
}

func TestAdminCanDoEverything(t *testing.T) {
	h, _ := usersRouter(t)
	admin := loginAs(t, h, "admin", "adminpw1")
	if rec := do(h, "GET", "/api/v1/users", "", admin); rec.Code != http.StatusOK {
		t.Errorf("admin GET users: status = %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/organizations", `{"kind":"tsn","name":"X"}`, admin); rec.Code == http.StatusForbidden {
		t.Errorf("admin POST organizations: forbidden")
	}
}

func TestUsersCRUD(t *testing.T) {
	h, _ := usersRouter(t)
	admin := loginAs(t, h, "admin", "adminpw1")

	rec := do(h, "POST", "/api/v1/users", `{"username":"buh","password":"buhpass12","role":"viewer"}`, admin)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "hash") {
		t.Errorf("response leaks password data: %s", rec.Body.String())
	}
	loginAs(t, h, "buh", "buhpass12")

	for name, body := range map[string]string{
		"short password": `{"username":"buh2","password":"short","role":"viewer"}`,
		"bad role":       `{"username":"buh2","password":"longenough1","role":"root"}`,
		"spaces":         `{"username":"b b","password":"longenough1","role":"viewer"}`,
	} {
		if rec := do(h, "POST", "/api/v1/users", body, admin); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422", name, rec.Code)
		}
	}
	if rec := do(h, "POST", "/api/v1/users", `{"username":"BUH","password":"buhpass12","role":"viewer"}`, admin); rec.Code != http.StatusConflict {
		t.Errorf("duplicate username: status = %d, want 409", rec.Code)
	}
}

func TestDisabledUserSessionStopsImmediately(t *testing.T) {
	h, f := usersRouter(t)
	admin := loginAs(t, h, "admin", "adminpw1")
	viewer := loginAs(t, h, "viewer", "viewerpw1")
	if rec := do(h, "GET", "/api/v1/organizations", "", viewer); rec.Code != http.StatusOK {
		t.Fatalf("before: %d", rec.Code)
	}
	id := f.users[2].ID
	rec := do(h, "PUT", "/api/v1/users/"+itoa(id), `{"role":"viewer","active":false}`, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, "GET", "/api/v1/organizations", "", viewer); rec.Code != http.StatusUnauthorized {
		t.Errorf("disabled user session: status = %d, want 401", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/auth/login", `{"username":"viewer","password":"viewerpw1"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("disabled user login: status = %d, want 401", rec.Code)
	}
}

func TestCannotRemoveLastAdmin(t *testing.T) {
	h, f := usersRouter(t)
	admin := loginAs(t, h, "admin", "adminpw1")
	id := itoa(f.users[1].ID)
	for _, body := range []string{`{"role":"viewer","active":true}`, `{"role":"admin","active":false}`} {
		if rec := do(h, "PUT", "/api/v1/users/"+id, body, admin); rec.Code != http.StatusConflict {
			t.Errorf("%s: status = %d, want 409", body, rec.Code)
		}
	}
	if rec := do(h, "PUT", "/api/v1/users/"+id, `{"role":"admin"}`, admin); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("missing active: status = %d, want 422", rec.Code)
	}
}

func TestChangeOwnPassword(t *testing.T) {
	h, _ := usersRouter(t)
	viewer := loginAs(t, h, "viewer", "viewerpw1")

	if rec := do(h, "PUT", "/api/v1/auth/password", `{"current_password":"wrong","new_password":"newpass123"}`, viewer); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("wrong current: status = %d, want 422", rec.Code)
	}
	if rec := do(h, "PUT", "/api/v1/auth/password", `{"current_password":"viewerpw1","new_password":"short"}`, viewer); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("short new: status = %d, want 422", rec.Code)
	}
	rec := do(h, "PUT", "/api/v1/auth/password", `{"current_password":"viewerpw1","new_password":"newpass123"}`, viewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("change: %d %s", rec.Code, rec.Body.String())
	}
	// Прежняя сессия завершена, новая (из ответа) работает, со старым паролем не войти.
	if rec := do(h, "GET", "/api/v1/organizations", "", viewer); rec.Code != http.StatusUnauthorized {
		t.Errorf("old session: status = %d, want 401", rec.Code)
	}
	var fresh *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			fresh = c
		}
	}
	if fresh == nil {
		t.Fatal("no refreshed cookie")
	}
	if rec := do(h, "GET", "/api/v1/organizations", "", fresh); rec.Code != http.StatusOK {
		t.Errorf("refreshed session: status = %d, want 200", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/auth/login", `{"username":"viewer","password":"viewerpw1"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("old password login: status = %d, want 401", rec.Code)
	}
	loginAs(t, h, "viewer", "newpass123")
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestLoginHistory(t *testing.T) {
	h, f := usersRouter(t)
	do(h, "POST", "/api/v1/auth/login", `{"username":"ghost","password":"x"}`)
	do(h, "POST", "/api/v1/auth/login", `{"username":"viewer","password":"wrong"}`)
	viewer := loginAs(t, h, "viewer", "viewerpw1")
	admin := loginAs(t, h, "admin", "adminpw1")

	if len(f.events) != 4 {
		t.Fatalf("events = %d, want 4", len(f.events))
	}
	if e := f.events[0]; e.Success || e.UserID != nil || e.Username != "ghost" {
		t.Errorf("unknown login event = %+v", e)
	}
	if e := f.events[2]; !e.Success || e.UserID == nil || e.Username != "viewer" {
		t.Errorf("viewer login event = %+v", e)
	}
	do(h, "GET", "/api/v1/auth/me", "", viewer)
	if f.touched[*f.events[2].UserID] == 0 {
		t.Error("активность пользователя не отмечена")
	}

	if rec := do(h, "GET", "/api/v1/users/logins", "", viewer); rec.Code != http.StatusForbidden {
		t.Errorf("viewer GET logins = %d, want 403", rec.Code)
	}
	rec := do(h, "GET", "/api/v1/users/logins", "", admin)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"username":"ghost"`) {
		t.Errorf("admin GET logins = %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Error("в истории входов не должно быть паролей")
	}
}
