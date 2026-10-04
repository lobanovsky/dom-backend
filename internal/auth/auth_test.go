package auth

import (
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func newTestAuth(t *testing.T) *Auth {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return New("admin", string(h), "secret", time.Hour)
}

func TestCheck(t *testing.T) {
	a := newTestAuth(t)
	if !a.Check("admin", "pw") {
		t.Error("valid credentials rejected")
	}
	if a.Check("admin", "bad") || a.Check("other", "pw") {
		t.Error("invalid credentials accepted")
	}
}

func TestTokenRoundTripAndTamper(t *testing.T) {
	a := newTestAuth(t)
	tok := a.Issue()
	if !a.Verify(tok) {
		t.Fatal("fresh token rejected")
	}
	if a.Verify(tok+"x") || a.Verify("garbage") || a.Verify("") {
		t.Error("tampered token accepted")
	}
}

func TestTokenExpires(t *testing.T) {
	a := newTestAuth(t)
	tok := a.Issue()
	a.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if a.Verify(tok) {
		t.Error("expired token accepted")
	}
}
