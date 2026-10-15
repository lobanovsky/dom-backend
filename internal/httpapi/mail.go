package httpapi

import (
	"context"
	"errors"
	"net/http"

	"dom-backend/internal/mailsync"
	"dom-backend/internal/model"
)

// MailStore — журнал запусков забора реестров из почты.
type MailStore interface {
	Runs(ctx context.Context, limit int) ([]model.MailRun, error)
}

// MailSyncer запускает забор реестров из почты в фоне.
type MailSyncer interface {
	Start(ctx context.Context, trigger string) (int64, error)
}

// MailInfo — настройки, известные только при старте (из окружения). Пароль сюда не попадает.
type MailInfo struct {
	Configured bool            // задан MAIL_USER и сервис собран
	User       string          // ящик, из которого забираются реестры
	Folder     string          // папка с письмами
	Every      string          // как часто проверять почту (например «1h0m0s»); пусто — только по кнопке
	SinceDays  int             // глубина поиска писем
	Ctx        context.Context // время жизни сервера: запуск в фоне не должен прерываться вместе с HTTP-запросом
}

type mailHandlers struct {
	store  MailStore
	syncer MailSyncer
	info   MailInfo
}

func (h mailHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/mail/status", h.status)
	mux.HandleFunc("POST /api/v1/mail/sync", h.sync)
}

type mailStatus struct {
	Configured bool            `json:"configured"`
	User       string          `json:"user,omitempty"`
	Folder     string          `json:"folder,omitempty"`
	Every      string          `json:"every,omitempty"`
	SinceDays  int             `json:"since_days,omitempty"`
	Running    bool            `json:"running"`
	Runs       []model.MailRun `json:"runs"`
}

func (h mailHandlers) status(w http.ResponseWriter, r *http.Request) {
	st := mailStatus{Configured: h.info.Configured, Runs: []model.MailRun{}}
	if h.info.Configured {
		st.User, st.Folder, st.Every, st.SinceDays = h.info.User, h.info.Folder, h.info.Every, h.info.SinceDays
		runs, err := h.store.Runs(r.Context(), 20)
		if err != nil {
			writeErr(w, err)
			return
		}
		st.Runs = runs
		for _, run := range runs {
			if run.FinishedAt == nil {
				st.Running = true
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, st)
}

// sync запускает проверку почты в фоне: 202 {run_id}; ход и итог — в status.runs.
func (h mailHandlers) sync(w http.ResponseWriter, _ *http.Request) {
	if !h.info.Configured {
		writeError(w, http.StatusConflict, "mail is not configured")
		return
	}
	id, err := h.syncer.Start(h.info.Ctx, "manual")
	switch {
	case errors.Is(err, mailsync.ErrBusy):
		writeError(w, http.StatusConflict, "mail sync is already running")
	case err != nil:
		writeErr(w, err)
	default:
		writeJSON(w, http.StatusAccepted, map[string]any{"run_id": id})
	}
}
