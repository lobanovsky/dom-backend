package residentmail

import "testing"

func TestConfigRejectsInsecureOriginAndHeaderInjection(t *testing.T) {
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
