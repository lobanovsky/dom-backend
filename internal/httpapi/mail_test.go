package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"dom-backend/internal/auth"
	"dom-backend/internal/mailsync"
	"dom-backend/internal/model"
)

type fakeMail struct {
	runs    []model.MailRun
	started int
	err     error
}

func (f *fakeMail) Runs(context.Context, int) ([]model.MailRun, error) { return f.runs, nil }
func (f *fakeMail) Start(context.Context, string) (int64, error) {
	f.started++
	return 42, f.err
}

func mailRouter(t *testing.T, configured bool, m *fakeMail) (http.Handler, *http.Cookie, *http.Cookie) {
	t.Helper()
	f := newFakeUsers()
	f.add("admin", "adminpw1", model.RoleAdmin)
	f.add("viewer", "viewerpw1", model.RoleViewer)
	h := NewRouter(Deps{Auth: auth.New(f, "secret", time.Hour), Users: f, Organizations: fakeOrganizations{},
		Mail: m, MailSyncer: m, MailInfo: MailInfo{Configured: configured, User: "reg@mail.example", Folder: "VTB", Every: "1h0m0s", SinceDays: 30, Ctx: context.Background()}})
	return h, loginAs(t, h, "admin", "adminpw1"), loginAs(t, h, "viewer", "viewerpw1")
}

func TestMailStatusAndSync(t *testing.T) {
	m := &fakeMail{runs: []model.MailRun{{ID: 1, Trigger: "schedule"}}}
	h, admin, viewer := mailRouter(t, true, m)

	rec := do(h, "GET", "/api/v1/mail/status", "", viewer)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"user":"reg@mail.example"`) || !strings.Contains(rec.Body.String(), `"running":true`) {
		t.Errorf("status = %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "password") {
		t.Error("в статусе почты не должно быть пароля")
	}

	if rec := do(h, "POST", "/api/v1/mail/sync", "", viewer); rec.Code != http.StatusForbidden || m.started != 0 {
		t.Errorf("viewer POST sync = %d, want 403 (started=%d)", rec.Code, m.started)
	}
	rec = do(h, "POST", "/api/v1/mail/sync", "", admin)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"run_id":42`) || m.started != 1 {
		t.Errorf("admin POST sync = %d %s", rec.Code, rec.Body)
	}

	m.err = mailsync.ErrBusy
	if rec := do(h, "POST", "/api/v1/mail/sync", "", admin); rec.Code != http.StatusConflict {
		t.Errorf("занято: %d, want 409", rec.Code)
	}
}

func TestMailNotConfigured(t *testing.T) {
	h, admin, _ := mailRouter(t, false, &fakeMail{})
	rec := do(h, "GET", "/api/v1/mail/status", "", admin)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"configured":false`) {
		t.Errorf("status = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/mail/sync", "", admin); rec.Code != http.StatusConflict {
		t.Errorf("sync без настройки = %d, want 409", rec.Code)
	}
}
