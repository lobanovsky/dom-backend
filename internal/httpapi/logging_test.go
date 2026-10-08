package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

type logLine struct {
	Level  string `json:"level"`
	Msg    string `json:"msg"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	IP     string `json:"ip"`
}

func capture(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func lines(t *testing.T, buf *bytes.Buffer) []logLine {
	t.Helper()
	var out []logLine
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var ll logLine
		if err := json.Unmarshal([]byte(l), &ll); err != nil {
			t.Fatalf("bad log line %q: %v", l, err)
		}
		out = append(out, ll)
	}
	return out
}

func TestRequestLogging(t *testing.T) {
	h := newTestRouter(t)
	buf := capture(t)
	cookie := login(t, h)
	buf.Reset()

	do(h, "GET", "/healthz", "")
	if buf.Len() != 0 {
		t.Errorf("/healthz must not be logged: %s", buf)
	}

	do(h, "GET", "/api/v1/auth/me", "")                                             // 401 «не вошли»: INFO
	do(h, "GET", "/api/v1/organizations", "")                                       // 401 без сессии: WARN
	do(h, "GET", "/api/v1/organizations?kind=%D0%98%D0%B2%D0%B0%D0%BD", "", cookie) // 200, запрос с поиском по фамилии
	req := httptest.NewRequest("GET", "/api/v1/organizations/404", nil)
	req.AddCookie(cookie)
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	h.ServeHTTP(httptest.NewRecorder(), req) // 404: WARN, адрес из X-Forwarded-For

	got := lines(t, buf)
	if len(got) != 4 {
		t.Fatalf("log lines = %d: %s", len(got), buf)
	}
	for i, want := range []struct {
		level  string
		status int
	}{{"INFO", 401}, {"WARN", 401}, {"INFO", 200}, {"WARN", 404}} {
		if got[i].Msg != "request" || got[i].Level != want.level || got[i].Status != want.status || got[i].Method != "GET" {
			t.Errorf("line %d = %+v, want %s %d", i, got[i], want.level, want.status)
		}
	}
	if got[3].IP != "203.0.113.7" {
		t.Errorf("client ip = %q, want the first X-Forwarded-For address", got[3].IP)
	}
	if strings.Contains(buf.String(), "kind=") || strings.Contains(buf.String(), "Иван") {
		t.Errorf("query strings must not be logged: %s", buf)
	}
}
