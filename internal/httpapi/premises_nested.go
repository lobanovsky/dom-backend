package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/model"
)

// PremisesOwnershipsStore и PremisesAccountsStore отдают вложенные данные помещения.
type PremisesOwnershipsStore interface {
	ListByPremises(ctx context.Context, premisesID int64) ([]model.OwnershipView, error)
}

type PremisesAccountsStore interface {
	ListByPremises(ctx context.Context, premisesID int64) ([]model.AccountView, error)
}

type premisesNestedHandlers struct {
	ownerships PremisesOwnershipsStore
	accounts   PremisesAccountsStore
}

func (h premisesNestedHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/premises/{id}/ownerships", h.listOwnerships)
	mux.HandleFunc("GET /api/v1/premises/{id}/accounts", h.listAccounts)
}

func (h premisesNestedHandlers) listOwnerships(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	items, err := h.ownerships.ListByPremises(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h premisesNestedHandlers) listAccounts(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	items, err := h.accounts.ListByPremises(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
