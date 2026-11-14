package httpapi

import (
	"context"
	"dom-backend/internal/model"
	"net/http"
	"regexp"
	"slices"
)

type GateStore interface {
	CreateBatch(context.Context, int64, model.GateBatchInput) ([]model.GateRequest, error)
	Properties(context.Context, int64) ([]model.GateProperty, error)
	List(context.Context, int64, model.GateFilter, int, int) ([]model.GateRequest, error)
	Create(context.Context, int64, model.GateInput) (model.GateRequest, error)
	Act(context.Context, int64, model.User, int64, string, string) (model.GateRequest, error)
	Events(context.Context, int64, int64) ([]model.GateEvent, error)
}
type gateHandlers struct{ s GateStore }

var gateEventPath = regexp.MustCompile(`^/api/v1/gate-access/[1-9][0-9]*/events$`)
var gateDecisionPath = regexp.MustCompile(`^/api/v1/gate-access/[1-9][0-9]*/decision$`)

func gateGuardAllowed(method, path string) bool {
	if (path == "/api/v1/auth/me" && (method == "GET" || method == "HEAD")) || (path == "/api/v1/auth/password" && method == "PUT") {
		return true
	}
	if method == "GET" || method == "HEAD" {
		return path == "/api/v1/gate-access" || gateEventPath.MatchString(path)
	}
	return method == "POST" && gateDecisionPath.MatchString(path)
}
func (h gateHandlers) registerAdmin(mux *http.ServeMux) {
	wrap := func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			u := currentUser(r)
			if u.Role != model.RoleAdmin && u.Role != model.RoleGuard {
				writeError(w, 403, "forbidden")
				return
			}
			fn(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/gate-access", wrap(h.list(false)))
	mux.HandleFunc("GET /api/v1/gate-access/{id}/events", wrap(h.events(false)))
	mux.HandleFunc("POST /api/v1/gate-access/{id}/decision", wrap(h.action(false)))
}
func (h gateHandlers) registerResident(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/resident/gate-access", h.list(true))
	mux.HandleFunc("GET /api/v1/resident/gate-access/properties", h.properties)
	mux.HandleFunc("POST /api/v1/resident/gate-access", h.create)
	mux.HandleFunc("POST /api/v1/resident/gate-access/batch", h.createBatch)
	mux.HandleFunc("POST /api/v1/resident/gate-access/{id}/action", h.action(true))
	mux.HandleFunc("GET /api/v1/resident/gate-access/{id}/events", h.events(true))
}
func gatePerson(w http.ResponseWriter, r *http.Request, resident bool) (int64, bool) {
	if !resident {
		return 0, true
	}
	person := residentAccount(r).PersonID
	if person <= 0 {
		writeError(w, 401, "authentication required")
		return 0, false
	}
	return person, true
}
func (h gateHandlers) list(resident bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		person, ok := gatePerson(w, r, resident)
		if !ok {
			return
		}
		p, ok := parseList(w, r, []string{"premises_id"}, []string{"status", "display_status", "zone", "q", "recipient", "phone", "vehicle", "premises"})
		if !ok {
			return
		}
		f := model.GateFilter{Status: p.texts["status"], DisplayStatus: p.texts["display_status"], Zone: p.texts["zone"], Q: p.texts["q"], Recipient: p.texts["recipient"], Phone: p.texts["phone"], Vehicle: p.texts["vehicle"], Premises: p.texts["premises"], PremisesID: p.ints["premises_id"]}
		if !slices.Contains([]string{"", "pending", "approved", "rejected"}, f.DisplayStatus) || p.Deleted || !slices.Contains([]string{"", "pending", "approved", "rejected", "cancelled", "revoked", "expired", "ineligible"}, f.Status) || !slices.Contains([]string{"", "courtyard", "parking"}, f.Zone) || len([]rune(f.Q)) > 120 {
			writeError(w, 422, "gate: invalid filters")
			return
		}
		items, err := h.s.List(r.Context(), person, f, p.Limit, p.Offset)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeList(w, items, p)
	}
}
func (h gateHandlers) properties(w http.ResponseWriter, r *http.Request) {
	person, ok := gatePerson(w, r, true)
	if !ok {
		return
	}
	items, err := h.s.Properties(r.Context(), person)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (h gateHandlers) create(w http.ResponseWriter, r *http.Request) {
	person, ok := gatePerson(w, r, true)
	if !ok {
		return
	}
	var in model.GateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	item, err := h.s.Create(r.Context(), person, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, item)
}
func (h gateHandlers) action(resident bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		person, ok := gatePerson(w, r, resident)
		if !ok {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in struct {
			Action string `json:"action"`
			Note   string `json:"note"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		item, err := h.s.Act(r.Context(), person, currentUser(r), id, in.Action, in.Note)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, item)
	}
}
func (h gateHandlers) events(resident bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		person, ok := gatePerson(w, r, resident)
		if !ok {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		items, err := h.s.Events(r.Context(), person, id)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	}
}

func (h gateHandlers) createBatch(w http.ResponseWriter, r *http.Request) {
	person, ok := gatePerson(w, r, true)
	if !ok {
		return
	}
	var in model.GateBatchInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	items, err := h.s.CreateBatch(r.Context(), person, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"items": items})
}
