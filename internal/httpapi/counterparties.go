package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

type CounterpartyStore interface {
	List(ctx context.Context, f model.CounterpartyFilter, limit, offset int) ([]model.Counterparty, error)
}

type counterpartyHandlers struct{ s CounterpartyStore }

func (h counterpartyHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/counterparties", h.list)
}

// list: контрагенты из платежей (получатели исходящих и плательщики входящих из выписок); ?direction=outgoing|incoming, ?q=.
func (h counterpartyHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, ok := parseListSpec(w, r, listSpec{Texts: []string{"q", "direction"}})
	if !ok {
		return
	}
	f := model.CounterpartyFilter{Q: p.Text("q")}
	if d := p.Text("direction"); d != nil {
		if *d != "outgoing" && *d != "incoming" {
			writeError(w, http.StatusBadRequest, "direction must be outgoing or incoming")
			return
		}
		f.Direction = *d
	}
	items, err := h.s.List(r.Context(), f, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}
