package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"dom-backend/internal/auth"
	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

type ResidentStore interface {
	Schedule(context.Context, int64) (model.ResidentScheduleSettings, error)
	SetSchedule(context.Context, model.ResidentSchedule) (model.ResidentScheduleSettings, error)
	DeleteWindow(context.Context, int64, model.Date) error
	SubmitReadings(context.Context, model.ResidentAccount, int64, []model.ResidentReadingInput) ([]model.MeterReading, error)
	ListAccess(context.Context, []int64) ([]model.ResidentAccess, error)
	ByEmail(context.Context, string) (model.ResidentAccount, error)
	Session(context.Context, string) (model.ResidentAccount, error)
	NewSession(context.Context, int64, string) (string, error)
	Logout(context.Context, string) error
	RequestToken(context.Context, string, string, string) error
	RedeemToken(context.Context, string, string) error
	ChangePassword(context.Context, int64, string, string) error
	Profile(context.Context, int64) (model.Person, error)
	Properties(context.Context, int64) ([]model.ResidentProperty, error)
	Meters(context.Context, int64) ([]model.Meter, error)
	Windows(context.Context, int64) ([]model.ResidentWindow, error)
	Readings(context.Context, model.ResidentAccount, int64, int, int) ([]model.ResidentReading, error)
	SubmitReading(context.Context, model.ResidentAccount, model.MeterReading, *int64) (model.MeterReading, error)
	Audit(context.Context, model.ResidentAccount, int64) ([]json.RawMessage, error)
	Approve(context.Context, int64, int64, string, bool) error
	SetWindow(context.Context, model.ResidentWindow) error
	Access(context.Context, int64) (map[string]any, error)
	Window(context.Context, int64) (*model.ResidentWindow, error)
}
type ResidentOptions struct {
	BaseURL     string
	MailEnabled bool
}
type residentKey struct{}

const residentCookie = "dom_resident_session"

type residentHandlers struct {
	store   ResidentStore
	gate    GateStore
	options ResidentOptions
	mu      sync.Mutex
	limits  map[string]residentLimit
	dummy   string
}
type residentLimit struct {
	count int
	until time.Time
}

func (h *residentHandlers) limited(key string, max int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if len(h.limits) > 10000 {
		for k, v := range h.limits {
			if !v.until.After(now) {
				delete(h.limits, k)
			}
		}
		if len(h.limits) > 10000 {
			return true
		}
	}
	v := h.limits[key]
	if !v.until.After(now) {
		v = residentLimit{until: now.Add(15 * time.Minute)}
	}
	v.count++
	h.limits[key] = v
	return v.count > max
}
func (h *residentHandlers) register(mux, admin *http.ServeMux) {
	h.limits = map[string]residentLimit{}
	h.dummy, _ = auth.HashPassword("resident dummy password")
	public := http.NewServeMux()
	public.HandleFunc("GET /api/v1/resident/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	public.HandleFunc("POST /api/v1/resident/auth/register", h.request("activate"))
	public.HandleFunc("POST /api/v1/resident/auth/forgot-password", h.request("reset"))
	public.HandleFunc("POST /api/v1/resident/auth/set-password", h.redeem)
	public.HandleFunc("POST /api/v1/resident/auth/login", h.login)
	private := http.NewServeMux()
	if h.gate != nil {
		gateHandlers{h.gate}.registerResident(private)
	}
	private.HandleFunc("GET /api/v1/resident/me", h.me)
	private.HandleFunc("GET /api/v1/resident/properties", h.properties)
	private.HandleFunc("GET /api/v1/resident/meters", h.meters)
	private.HandleFunc("GET /api/v1/resident/windows", h.windows)
	private.HandleFunc("GET /api/v1/resident/meters/{id}/readings", h.readings)
	private.HandleFunc("GET /api/v1/resident/meters/{id}/history", h.audit)
	private.HandleFunc("POST /api/v1/resident/meters/{id}/readings", h.submit)
	private.HandleFunc("POST /api/v1/resident/properties/{id}/readings", h.submitBatch)
	private.HandleFunc("POST /api/v1/resident/auth/logout", h.logout)
	private.HandleFunc("PUT /api/v1/resident/auth/password", h.password)
	public.Handle("/api/v1/resident/", h.require(private))
	mux.Handle("/api/v1/resident/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, e := url.Parse(origin)
				scheme := "http"
				if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
					scheme = "https"
				}
				if e != nil || u.Host != r.Host || u.Scheme != scheme {
					writeError(w, 403, "origin not allowed")
					return
				}
			}
			if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
				writeError(w, 403, "origin not allowed")
				return
			}
			ip := clientIP(r)
			if h.limited("ip:"+ip, 120) {
				writeError(w, 429, "too many attempts; try later")
				return
			}
		}
		public.ServeHTTP(w, r)
	}))
	admin.HandleFunc("GET /api/v1/resident-access", h.listAccess)
	admin.HandleFunc("GET /api/v1/resident-access/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !residentAdmin(w, r) {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		v, e := h.store.Access(r.Context(), id)
		if e != nil {
			writeErr(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	admin.HandleFunc("GET /api/v1/resident-windows/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !residentAdmin(w, r) {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		v, e := h.store.Window(r.Context(), id)
		if e != nil {
			writeErr(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	admin.HandleFunc("PUT /api/v1/resident-access/{id}", h.approve)
	admin.HandleFunc("POST /api/v1/resident-access/{id}/invite", h.invite)
	admin.HandleFunc("PUT /api/v1/resident-windows/{id}", h.setWindow)
	admin.HandleFunc("DELETE /api/v1/resident-windows/{id}", h.deleteWindow)
	admin.HandleFunc("GET /api/v1/resident-schedules/{id}", h.schedule)
	admin.HandleFunc("PUT /api/v1/resident-schedules/{id}", h.setSchedule)
}
func residentAccount(r *http.Request) model.ResidentAccount {
	a, _ := r.Context().Value(residentKey{}).(model.ResidentAccount)
	return a
}
func (h *residentHandlers) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie(residentCookie)
		if e != nil {
			writeError(w, 401, "authentication required")
			return
		}
		a, e := h.store.Session(r.Context(), c.Value)
		if errors.Is(e, store.ErrNotFound) {
			writeError(w, 401, "authentication required")
			return
		}
		if e != nil {
			writeErr(w, e)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), residentKey{}, a)))
	})
}
func residentEmail(email string) (string, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	a, e := mail.ParseAddress(email)
	return email, e == nil && a.Address == email && len(email) <= 254
}
func (h *residentHandlers) request(purpose string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email string `json:"email"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		email, ok := residentEmail(in.Email)
		if !ok {
			writeError(w, 422, "email: invalid email")
			return
		}
		if !h.options.MailEnabled {
			writeError(w, 503, "email delivery is not configured; contact management")
			return
		}
		if !h.limited("mail:"+store.ResidentTokenHash(email), 3) {
			if e := h.store.RequestToken(r.Context(), email, purpose, h.options.BaseURL); e != nil {
				writeErr(w, e)
				return
			}
		}
		writeJSON(w, 200, map[string]string{"message": "Если доступ к этому адресу подтверждён, мы отправим письмо с дальнейшими действиями."})
	}
}
func (h *residentHandlers) redeem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if len(in.Token) != 64 {
		writeError(w, 422, "invalid or expired link")
		return
	}
	if err := model.ValidatePassword("password", in.Password); err != nil {
		writeErr(w, err)
		return
	}
	hash, e := auth.HashPassword(in.Password)
	if e != nil {
		writeErr(w, e)
		return
	}
	if e = h.store.RedeemToken(r.Context(), in.Token, hash); e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
func (h *residentHandlers) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	email, _ := residentEmail(in.Email)
	if h.limited("login:"+store.ResidentTokenHash(email), 15) {
		writeError(w, 429, "too many attempts; try later")
		return
	}
	a, e := h.store.ByEmail(r.Context(), email)
	hash := a.PasswordHash
	if errors.Is(e, store.ErrNotFound) {
		hash = h.dummy
	} else if e != nil {
		writeErr(w, e)
		return
	}
	valid := auth.CheckPassword(hash, in.Password)
	if !valid || e != nil {
		writeError(w, 401, "invalid credentials")
		return
	}
	raw, e := h.store.NewSession(r.Context(), a.ID, a.PasswordHash)
	if e != nil {
		if errors.Is(e, store.ErrNotFound) {
			writeError(w, 401, "invalid credentials")
			return
		}
		writeErr(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: residentCookie, Value: raw, Path: "/api/v1/resident", HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode, MaxAge: 7 * 24 * 3600})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
func (h *residentHandlers) logout(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie(residentCookie)
	if c != nil {
		if e := h.store.Logout(r.Context(), c.Value); e != nil {
			writeErr(w, e)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: residentCookie, Path: "/api/v1/resident", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
func (h *residentHandlers) password(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	a := residentAccount(r)
	if !auth.CheckPassword(a.PasswordHash, in.Current) {
		writeError(w, 422, "current_password: is incorrect")
		return
	}
	if e := model.ValidatePassword("new_password", in.New); e != nil {
		writeErr(w, e)
		return
	}
	hash, e := auth.HashPassword(in.New)
	if e != nil {
		writeErr(w, e)
		return
	}
	if e = h.store.ChangePassword(r.Context(), a.ID, a.PasswordHash, hash); e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
func (h *residentHandlers) me(w http.ResponseWriter, r *http.Request) {
	a := residentAccount(r)
	p, e := h.store.Profile(r.Context(), a.PersonID)
	if e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"account": map[string]any{"email": a.Email}, "person": p})
}
func (h *residentHandlers) properties(w http.ResponseWriter, r *http.Request) {
	v, e := h.store.Properties(r.Context(), residentAccount(r).PersonID)
	if e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h *residentHandlers) meters(w http.ResponseWriter, r *http.Request) {
	v, e := h.store.Meters(r.Context(), residentAccount(r).PersonID)
	if e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h *residentHandlers) windows(w http.ResponseWriter, r *http.Request) {
	v, e := h.store.Windows(r.Context(), residentAccount(r).PersonID)
	if e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h *residentHandlers) readings(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, ok := parseList(w, r, nil, nil)
	if !ok {
		return
	}
	v, e := h.store.Readings(r.Context(), residentAccount(r), id, p.Limit, p.Offset)
	if e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h *residentHandlers) audit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, e := h.store.Audit(r.Context(), residentAccount(r), id)
	if e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h *residentHandlers) submit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Period   model.Date `json:"period"`
		T1       *float64   `json:"t1"`
		T2       *float64   `json:"t2"`
		T3       *float64   `json:"t3"`
		Comment  *string    `json:"comment"`
		Revision *int64     `json:"revision"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.T1 == nil {
		writeError(w, 422, "t1: is required")
		return
	}
	if in.Comment != nil && len([]rune(*in.Comment)) > 2000 {
		writeError(w, 422, "comment: too long")
		return
	}
	reading := model.MeterReading{MeterID: id, Period: in.Period, T1: *in.T1, T2: in.T2, T3: in.T3, Comment: in.Comment, Source: "resident", ReadingDate: model.NewDate(2000, 1, 1)}
	if e := reading.Validate(); e != nil {
		writeErr(w, e)
		return
	}
	if in.Period.IsZero() || in.Period.Day() != 1 {
		writeError(w, 422, "period: expected first day of month")
		return
	}
	v, e := h.store.SubmitReading(r.Context(), residentAccount(r), reading, in.Revision)
	if e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func residentAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !currentUser(r).IsAdmin() {
		writeError(w, 403, "forbidden")
		return false
	}
	return true
}
func (h *residentHandlers) approve(w http.ResponseWriter, r *http.Request) {
	if !residentAdmin(w, r) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Email   string `json:"email"`
		Enabled bool   `json:"enabled"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	email, ok := residentEmail(in.Email)
	if !ok {
		writeError(w, 422, "email: invalid email")
		return
	}
	if e := h.store.Approve(r.Context(), id, currentUser(r).ID, email, in.Enabled); e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
func (h *residentHandlers) invite(w http.ResponseWriter, r *http.Request) {
	if !residentAdmin(w, r) {
		return
	}
	if !h.options.MailEnabled {
		writeError(w, 503, "email delivery is not configured; contact management")
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	email, ok := residentEmail(in.Email)
	if !ok {
		writeError(w, 422, "email: invalid email")
		return
	}
	if e := h.store.Approve(r.Context(), id, currentUser(r).ID, email, true); e != nil {
		writeErr(w, e)
		return
	}
	if e := h.store.RequestToken(r.Context(), email, "activate", h.options.BaseURL); e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
func (h *residentHandlers) setWindow(w http.ResponseWriter, r *http.Request) {
	if !residentAdmin(w, r) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Enabled  *bool      `json:"enabled"`
		Period   model.Date `json:"period"`
		OpensOn  model.Date `json:"opens_on"`
		ClosesOn model.Date `json:"closes_on"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Period.IsZero() || in.OpensOn.IsZero() || in.ClosesOn.IsZero() || in.Period.Day() != 1 || in.ClosesOn.Before(in.OpensOn.Time) {
		writeError(w, 422, "invalid reading window")
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	e := h.store.SetWindow(r.Context(), model.ResidentWindow{BuildingID: id, Period: in.Period, OpensOn: in.OpensOn, ClosesOn: in.ClosesOn, Enabled: enabled})
	if e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *residentHandlers) listAccess(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !residentAdmin(w, r) {
		return
	}
	if len(r.URL.Query()) != 1 || len(r.URL.Query()["person_ids"]) != 1 {
		writeError(w, 400, "person_ids: expected comma-separated ids")
		return
	}
	parts := strings.Split(r.URL.Query().Get("person_ids"), ",")
	if len(parts) > 200 {
		writeError(w, 400, "person_ids: at most 200 ids")
		return
	}
	ids := make([]int64, 0, len(parts))
	seen := map[int64]bool{}
	for _, part := range parts {
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id < 1 {
			writeError(w, 400, "person_ids: invalid id")
			return
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	result, err := h.store.ListAccess(r.Context(), ids)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (h *residentHandlers) schedule(w http.ResponseWriter, r *http.Request) {
	if !residentAdmin(w, r) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	v, err := h.store.Schedule(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h *residentHandlers) setSchedule(w http.ResponseWriter, r *http.Request) {
	if !residentAdmin(w, r) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Enabled   *bool `json:"enabled"`
		OpensDay  int   `json:"opens_day"`
		ClosesDay int   `json:"closes_day"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Enabled == nil {
		writeError(w, 422, "enabled: is required")
		return
	}
	v, err := h.store.SetSchedule(r.Context(), model.ResidentSchedule{BuildingID: id, Enabled: *in.Enabled, OpensDay: in.OpensDay, ClosesDay: in.ClosesDay})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h *residentHandlers) deleteWindow(w http.ResponseWriter, r *http.Request) {
	if !residentAdmin(w, r) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := time.Parse("2006-01", r.URL.Query().Get("period"))
	if err != nil {
		writeError(w, 400, "period: expected YYYY-MM")
		return
	}
	if err = h.store.DeleteWindow(r.Context(), id, model.Date{Time: p}); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
func (h *residentHandlers) submitBatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Readings []model.ResidentReadingInput `json:"readings"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if len(in.Readings) == 0 || len(in.Readings) > 100 {
		writeError(w, 422, "readings: expected 1 to 100 readings")
		return
	}
	for _, entry := range in.Readings {
		if _, err := entry.Reading(); err != nil {
			writeJSON(w, 422, map[string]any{"error": err.Error(), "meter_id": entry.MeterID})
			return
		}
	}
	result, err := h.store.SubmitReadings(r.Context(), residentAccount(r), id, in.Readings)
	if err != nil {
		var readingErr *store.ResidentReadingError
		if errors.As(err, &readingErr) {
			status := 0
			switch {
			case errors.Is(err, store.ErrConflict):
				status = 409
			case errors.Is(err, store.ErrNotFound):
				status = 404
			case errors.Is(err, store.ErrInvalid):
				status = 422
			}
			if status != 0 {
				writeJSON(w, status, map[string]any{"error": readingErr.Err.Error(), "meter_id": readingErr.MeterID})
				return
			}
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"readings": result, "accepted_count": len(result)})
}
