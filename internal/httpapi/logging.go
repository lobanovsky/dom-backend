package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// statusRecorder запоминает код ответа и число записанных байт.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// logRequests пишет по строке на запрос: метод, путь, код, время, размер тела запроса и ответа, адрес клиента.
// Тела и параметры запроса (в них бывают фамилии из поиска) не логируются. /healthz пропускается, чтобы не шуметь.
// Уровень: 5xx — ERROR, 4xx — WARN (401 на /auth/me — обычное «не вошли», INFO), остальное INFO.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		level := slog.LevelInfo
		switch {
		case rec.status >= 500:
			level = slog.LevelError
		case rec.status >= 400 && !(rec.status == http.StatusUnauthorized && r.URL.Path == "/api/v1/auth/me"):
			level = slog.LevelWarn
		}
		slog.Log(r.Context(), level, "request",
			"method", r.Method, "path", r.URL.Path, "status", rec.status,
			"ms", time.Since(start).Milliseconds(), "req_bytes", r.ContentLength, "resp_bytes", rec.bytes, "ip", clientIP(r))
	})
}

// clientIP: за Traefik/Caddy настоящий адрес приходит в X-Forwarded-For.
func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}
