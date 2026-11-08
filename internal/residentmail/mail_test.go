package residentmail

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigRejectsInsecureOriginAndHeaderInjection(t *testing.T) {
	t.Setenv("MAIL_USER", "")
	t.Setenv("MAIL_PASSWORD_FILE", "")
	t.Setenv("RESIDENT_SMTP_ADDR", "smtp.example.test:587")
	t.Setenv("RESIDENT_MAIL_FROM", "noreply@example.test")
	for _, origin := range []string{"http://lk.example.test", "https://lk.example.test/?evil=1", "https://user:pass@lk.example.test", "https://lk.example.test/#evil"} {
		t.Setenv("RESIDENT_BASE_URL", origin)
		if _, e := Load(); e == nil {
			t.Fatalf("accepted %s", origin)
		}
	}
	t.Setenv("RESIDENT_BASE_URL", "https://lk.example.test")
	t.Setenv("RESIDENT_MAIL_FROM", "a@example.test\r\nBcc: b@example.test")
	if _, e := Load(); e == nil {
		t.Fatal("header injection accepted")
	}
	t.Setenv("RESIDENT_MAIL_FROM", "noreply@example.test")
	c, e := Load()
	if e != nil || !c.Enabled() {
		t.Fatal(e)
	}
}

func TestSMTPUsesSharedMailboxCredentials(t *testing.T) {
	t.Setenv("RESIDENT_SMTP_ADDR", "smtp.example.test:465")
	t.Setenv("RESIDENT_SMTP_TLS", "implicit")
	t.Setenv("RESIDENT_BASE_URL", "https://resident.example.test")
	t.Setenv("RESIDENT_MAIL_FROM", "mail@example.test")
	t.Setenv("MAIL_USER", "mail@example.test")
	passwordFile := filepath.Join(t.TempDir(), "mail.pass")
	if err := os.WriteFile(passwordFile, []byte("application-password\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAIL_PASSWORD_FILE", passwordFile)
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.Username != "mail@example.test" || config.Password != "application-password" || !config.ImplicitTLS {
		t.Fatal("SMTP did not load shared mailbox credentials")
	}
	t.Setenv("MAIL_PASSWORD_FILE", "")
	if _, err := Load(); err == nil {
		t.Fatal("SMTP accepted missing shared password file")
	}
}
