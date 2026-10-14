package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dom-backend/internal/model"
)

func TestUsers(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	users := NewUsers(pool)

	name := "ut_" + strings.ToLower(t.Name())
	cleanup := func() { pool.Exec(ctx, `DELETE FROM users WHERE username LIKE 'ut\_%'`) } //nolint:errcheck // best-effort test cleanup
	cleanup()
	t.Cleanup(cleanup)

	u, err := users.Create(ctx, name+"_A", "hash1", model.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	if !u.Active || u.Role != model.RoleViewer || u.TokenVersion != 0 {
		t.Errorf("created: %+v", u)
	}

	// Логин находится без учёта регистра и уникален без учёта регистра.
	found, err := users.ByUsername(ctx, strings.ToUpper(name+"_A"))
	if err != nil || found.ID != u.ID {
		t.Errorf("ByUsername case-insensitive: %v %+v", err, found)
	}
	var se *Error
	if _, err := users.Create(ctx, strings.ToUpper(name+"_A"), "h", model.RoleViewer); !errors.As(err, &se) || !errors.Is(se.Kind, ErrConflict) || !strings.Contains(se.Msg, "users_username_key") {
		t.Errorf("duplicate username: %v", err)
	}
	if _, err := users.ByUsername(ctx, "ut_nobody"); !errors.As(err, &se) || !errors.Is(se.Kind, ErrNotFound) {
		t.Errorf("missing user: %v", err)
	}
	if _, err := users.Create(ctx, name+"_B", "h", "root"); err == nil {
		t.Error("unknown role accepted")
	}

	// Смена пароля увеличивает версию токена; отключение — тоже, повторное включение версию не возвращает.
	p, err := users.SetPassword(ctx, u.ID, "hash2")
	if err != nil || p.PasswordHash != "hash2" || p.TokenVersion != 1 {
		t.Errorf("SetPassword: %v %+v", err, p)
	}
	d, err := users.Update(ctx, u.ID, model.RoleViewer, false)
	if err != nil || d.Active || d.TokenVersion != 2 {
		t.Errorf("disable: %v %+v", err, d)
	}
	e, err := users.Update(ctx, u.ID, model.RoleViewer, true)
	if err != nil || !e.Active || e.TokenVersion != 2 {
		t.Errorf("enable: %v %+v", err, e)
	}
	if _, err := users.Update(ctx, 1<<40, model.RoleViewer, true); !errors.As(err, &se) || !errors.Is(se.Kind, ErrNotFound) {
		t.Errorf("update missing: %v", err)
	}
}

func TestUsersLastAdminGuard(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	users := NewUsers(pool)

	var others int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE role = 'admin' AND active AND username NOT LIKE 'ut\_%'`).Scan(&others); err != nil {
		t.Fatal(err)
	}
	if others > 0 {
		t.Skip("в БД уже есть другие администраторы: охрану «последнего админа» проверяем только на чистой тестовой БД")
	}
	cleanup := func() { pool.Exec(ctx, `DELETE FROM users WHERE username LIKE 'ut\_%'`) } //nolint:errcheck // best-effort test cleanup
	cleanup()
	t.Cleanup(cleanup)

	a, err := users.Create(ctx, "ut_admin_a", "h", model.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	var se *Error
	for _, c := range []struct {
		role   string
		active bool
	}{{model.RoleViewer, true}, {model.RoleAdmin, false}} {
		if _, err := users.Update(ctx, a.ID, c.role, c.active); !errors.As(err, &se) || !errors.Is(se.Kind, ErrConflict) {
			t.Errorf("last admin -> %+v: %v", c, err)
		}
	}
	b, err := users.Create(ctx, "ut_admin_b", "h", model.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.Update(ctx, a.ID, model.RoleViewer, true); err != nil {
		t.Errorf("demote with another admin present: %v", err)
	}
	if _, err := users.Update(ctx, b.ID, model.RoleAdmin, false); !errors.As(err, &se) || !errors.Is(se.Kind, ErrConflict) {
		t.Errorf("b is now the last admin: %v", err)
	}
}

func TestEnsureAdmin(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	users := NewUsers(pool)
	cleanup := func() { pool.Exec(ctx, `DELETE FROM users WHERE username LIKE 'ut\_%'`) } //nolint:errcheck // best-effort test cleanup
	cleanup()
	t.Cleanup(cleanup)

	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	// На пустой таблице администратор создаётся один раз; на непустой (обычная dev-БД) не создаётся никогда.
	created, err := users.EnsureAdmin(ctx, "ut_seed", "h")
	if err != nil || created != (total == 0) {
		t.Errorf("first call: created=%v err=%v (users before: %d)", created, err, total)
	}
	if created, err = users.EnsureAdmin(ctx, "ut_seed2", "h"); err != nil || created {
		t.Errorf("second call: created=%v err=%v", created, err)
	}
}
