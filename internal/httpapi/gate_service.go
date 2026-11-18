package httpapi

import (
	"context"
	"dom-backend/internal/model"
	"net/http"
	"strings"
	"unicode"
)

type gateServiceStore interface {
	ServiceAccesses(context.Context) ([]model.GateServiceAccess, error)
	CreateServiceAccess(context.Context, model.User, model.GateServiceAccessInput) (model.GateServiceAccess, error)
	RevokeServiceZone(context.Context, model.User, int64, string) error
}

type gatePhoneLookupStore interface {
	LookupGatePhone(context.Context, string) ([]model.GatePhoneLookupEntry, error)
	LookupGateRecipient(context.Context, string) ([]model.GatePhoneLookupEntry, error)
}

func (h gateHandlers) registerService(mux *http.ServeMux) {
	s, ok := h.s.(gateServiceStore)
	if !ok {
		return
	}
	admin := func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !currentUser(r).IsAdmin() {
				writeError(w, 403, "forbidden")
				return
			}
			fn(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/gate-service-accesses", admin(func(w http.ResponseWriter, r *http.Request) {
		items, err := s.ServiceAccesses(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	}))
	mux.HandleFunc("POST /api/v1/gate-service-accesses", admin(func(w http.ResponseWriter, r *http.Request) {
		var in model.GateServiceAccessInput
		if !decodeJSON(w, r, &in) {
			return
		}
		in.Normalize()
		if err := in.Validate(); err != nil {
			writeErr(w, err)
			return
		}
		item, err := s.CreateServiceAccess(r.Context(), currentUser(r), in)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 201, item)
	}))
	mux.HandleFunc("POST /api/v1/gate-service-accesses/{id}/{zone}/revoke", admin(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		if err := s.RevokeServiceZone(r.Context(), currentUser(r), id, r.PathValue("zone")); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

func (h gateHandlers) lookupPhone(w http.ResponseWriter, r *http.Request) {
	s, ok := h.s.(gatePhoneLookupStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Поиск доступов недоступен")
		return
	}
	phoneInput := strings.TrimSpace(r.URL.Query().Get("phone"))
	recipient := strings.TrimSpace(r.URL.Query().Get("recipient"))
	if recipient != "" {
		if phoneInput != "" {
			writeError(w, http.StatusBadRequest, "Ищите либо по телефону, либо по ФИО")
			return
		}
		if len([]rune(recipient)) < 2 || len([]rune(recipient)) > 120 || strings.IndexFunc(recipient, unicode.IsControl) >= 0 {
			writeError(w, http.StatusBadRequest, "Введите не менее двух символов ФИО")
			return
		}
		items, err := s.LookupGateRecipient(r.Context(), recipient)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"recipient": recipient, "items": items})
		return
	}
	phone := model.NormalizePhone(phoneInput)
	if len(phone) < 11 || len(phone) > 15 {
		writeError(w, http.StatusBadRequest, "Введите номер телефона полностью")
		return
	}
	for _, digit := range phone {
		if digit < '0' || digit > '9' {
			writeError(w, http.StatusBadRequest, "Введите номер телефона только цифрами")
			return
		}
	}
	items, err := s.LookupGatePhone(r.Context(), phone)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"phone": phone, "items": items})
}
