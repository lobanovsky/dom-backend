package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"dom-backend/internal/model"
	"dom-backend/internal/sbersync"
)

// SberStore — токены и журнал запусков Sber API.
type SberStore interface {
	Tokens(ctx context.Context) (model.SberTokens, error)
	SaveTokens(ctx context.Context, t model.SberTokens) error
	TokensUpdatedAt(ctx context.Context) (*time.Time, error)
	Runs(ctx context.Context, limit int) ([]model.SberRun, error)
}

// SberSyncer запускает получение выписок в фоне.
type SberSyncer interface {
	Start(ctx context.Context, trigger string, from, to time.Time) (int64, error)
}

// SberInfo — настройки, известные только при старте (из окружения).
type SberInfo struct {
	Configured  bool            // заданы client_id/секрет/сертификат и сервис собран
	CertExpires *time.Time      // срок действия клиентского сертификата
	SyncAt      string          // «ЧЧ:ММ» ежедневного опроса; пусто — только по кнопке
	SyncTZ      string          // часовой пояс SyncAt
	Days        int             // сколько последних дней запрашивает опрос по расписанию
	Ctx         context.Context // время жизни сервера: запуск в фоне не должен прерываться вместе с HTTP-запросом
}

type sberHandlers struct {
	store  SberStore
	syncer SberSyncer
	info   SberInfo
}

func (h sberHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/sber/status", h.status)
	mux.HandleFunc("POST /api/v1/sber/sync", h.sync)
	mux.HandleFunc("PUT /api/v1/sber/tokens", h.setTokens)
}

type sberStatus struct {
	Configured      bool            `json:"configured"`
	TokensSet       bool            `json:"tokens_set"`
	TokensUpdatedAt *time.Time      `json:"tokens_updated_at"`
	CertExpires     *time.Time      `json:"cert_expires_at"`
	ScheduleAt      string          `json:"schedule_at"`
	ScheduleTZ      string          `json:"schedule_tz"`
	Running         bool            `json:"running"`
	Runs            []model.SberRun `json:"runs"`
}

func (h sberHandlers) status(w http.ResponseWriter, r *http.Request) {
	st := sberStatus{Configured: h.info.Configured, CertExpires: h.info.CertExpires, Runs: []model.SberRun{}}
	if h.info.SyncAt != "" {
		st.ScheduleAt, st.ScheduleTZ = h.info.SyncAt, h.info.SyncTZ
	}
	at, err := h.store.TokensUpdatedAt(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	st.TokensSet, st.TokensUpdatedAt = at != nil, at
	runs, err := h.store.Runs(r.Context(), 20)
	if err != nil {
		writeErr(w, err)
		return
	}
	st.Runs = runs
	for _, run := range runs {
		if run.FinishedAt == nil {
			st.Running = true
		}
	}
	writeJSON(w, http.StatusOK, st)
}

type syncRequest struct {
	DateFrom *model.Date `json:"date_from"`
	DateTo   *model.Date `json:"date_to"`
}

// maxSyncDays ограничивает ручной запрос: по одному запросу к банку на день и счёт.
const maxSyncDays = 400

func (h sberHandlers) sync(w http.ResponseWriter, r *http.Request) {
	if !h.info.Configured {
		writeError(w, http.StatusConflict, "sber api is not configured")
		return
	}
	var req syncRequest
	if r.ContentLength != 0 && !decodeJSON(w, r, &req) {
		return
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	to := today
	if req.DateTo != nil {
		to = req.DateTo.Time
	}
	days := h.info.Days
	if days < 1 {
		days = 3
	}
	from := to.AddDate(0, 0, -(days - 1))
	if req.DateFrom != nil {
		from = req.DateFrom.Time
	}
	switch {
	case to.After(today):
		writeError(w, http.StatusUnprocessableEntity, "date_to: must not be in the future")
		return
	case from.After(to):
		writeError(w, http.StatusUnprocessableEntity, "date_from: must not be after date_to")
		return
	case int(to.Sub(from).Hours()/24)+1 > maxSyncDays:
		writeError(w, http.StatusUnprocessableEntity, "period: too long")
		return
	}
	id, err := h.syncer.Start(h.info.Ctx, "manual", from, to)
	switch {
	case errors.Is(err, sbersync.ErrBusy):
		writeError(w, http.StatusConflict, "sber sync is already running")
	case errors.Is(err, sbersync.ErrNotConfigured):
		writeError(w, http.StatusConflict, "sber tokens are not set")
	case err != nil:
		writeErr(w, err)
	default:
		writeJSON(w, http.StatusAccepted, map[string]any{"run_id": id})
	}
}

func (h sberHandlers) setTokens(w http.ResponseWriter, r *http.Request) {
	if !h.info.Configured {
		writeError(w, http.StatusConflict, "sber api is not configured")
		return
	}
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.RefreshToken = strings.TrimSpace(req.RefreshToken)
	if req.RefreshToken == "" {
		writeError(w, http.StatusUnprocessableEntity, "refresh_token: must not be empty")
		return
	}
	// Нулевой срок access_token заставит клиент сразу обменять refresh_token на свежую пару.
	if err := h.store.SaveTokens(r.Context(), model.SberTokens{RefreshToken: req.RefreshToken}); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
