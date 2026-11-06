package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"dom-backend/internal/model"
	"dom-backend/internal/ownersregistry"
)

type OwnersRegistryStore interface {
	Build(ctx context.Context, buildingID int64, day time.Time) (model.OwnersRegistry, error)
}

type ownersRegistryHandlers struct{ s OwnersRegistryStore }

func (h ownersRegistryHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/buildings/{id}/owners-registry", h.pdf)
}

// pdf отдаёт реестр собственников дома в PDF; необязательный параметр date (YYYY-MM-DD) — на какую дату, по умолчанию сегодня.
func (h ownersRegistryHandlers) pdf(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	day := time.Now()
	if v := r.URL.Query().Get("date"); v != "" {
		d, err := time.ParseInLocation("2006-01-02", v, time.Local)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid date, expected YYYY-MM-DD")
			return
		}
		day = d
	}
	reg, err := h.s.Build(r.Context(), id, day)
	if err != nil {
		writeErr(w, err)
		return
	}
	data, err := ownersregistry.Render(reg)
	if err != nil {
		writeErr(w, err)
		return
	}
	name := "Реестр собственников " + reg.Day + ".pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}
