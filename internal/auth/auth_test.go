package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

type fakeUsers map[int64]*model.User

func (f fakeUsers) ByUsername(_ context.Context, name string) (model.User, error) {
	for _, u := range f {
		if u.Username == name {
			return *u, nil
		}
	}
	return model.User{}, &store.Error{Kind: store.ErrNotFound, Msg: "not found"}
}

func (f fakeUsers) ByID(_ context.Context, id int64) (model.User, error) {
	if u, ok := f[id]; ok {
		return *u, nil
	}
	return model.User{}, &store.Error{Kind: store.ErrNotFound, Msg: "not found"}
}

func newTestAuth(t *testing.T) (*Auth, fakeUsers) {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	users := fakeUsers{1: {ID: 1, Username: "admin", Role: model.RoleAdmin, Active: true, PasswordHash: string(h)}}
	return New(users, "secret", time.Hour), users
}

func TestLogin(t *testing.T) {
	a, users := newTestAuth(t)
	ctx := context.Background()
	if _, err := a.Login(ctx, "admin", "pw"); err != nil {
		t.Errorf("valid credentials rejected: %v", err)
	}
	for _, c := range [][2]string{{"admin", "bad"}, {"other", "pw"}} {
		if _, err := a.Login(ctx, c[0], c[1]); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("Login(%q) = %v, want ErrInvalidCredentials", c[0], err)
		}
	}
	users[1].Active = false
	if _, err := a.Login(ctx, "admin", "pw"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("inactive user logged in: %v", err)
	}
}

func TestTokenRoundTripAndTamper(t *testing.T) {
	a, users := newTestAuth(t)
	ctx := context.Background()
	tok := a.Issue(*users[1])
	u, err := a.Authenticate(ctx, tok)
	if err != nil || u.ID != 1 {
		t.Fatalf("fresh token rejected: %v", err)
	}
	for _, bad := range []string{tok + "x", "garbage", "", "a.b.c.d"} {
		if _, err := a.Authenticate(ctx, bad); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("token %q accepted: %v", bad, err)
		}
	}
}

func TestTokenExpires(t *testing.T) {
	a, users := newTestAuth(t)
	tok := a.Issue(*users[1])
	a.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := a.Authenticate(context.Background(), tok); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("expired token accepted: %v", err)
	}
}

func TestTokenInvalidatedByUserChanges(t *testing.T) {
	a, users := newTestAuth(t)
	ctx := context.Background()
	tok := a.Issue(*users[1])

	users[1].TokenVersion++ // смена пароля
	if _, err := a.Authenticate(ctx, tok); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("token with stale version accepted: %v", err)
	}
	tok = a.Issue(*users[1])
	users[1].Active = false // отключение
	if _, err := a.Authenticate(ctx, tok); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("token of inactive user accepted: %v", err)
	}
	delete(users, 1)
	if _, err := a.Authenticate(ctx, tok); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("token of missing user accepted: %v", err)
	}
}
