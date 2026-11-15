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
	AdminCreate(context.Context, model.User, model.GateInput) (model.GateRequest, error)
	Properties(context.Context, int64) ([]model.GateProperty, error)
	List(context.Context, int64, model.GateFilter, int, int) ([]model.GateRequest, error)
	Create(context.Context, int64, model.GateInput) (model.GateRequest, error)
	Edit(context.Context, int64, model.User, int64, model.GateEditInput) (model.GateRequest, error)
	Act(context.Context, int64, model.User, int64, string, string) (model.GateRequest, error)
	Events(context.Context, int64, int64) ([]model.GateEvent, error)
	DeleteRejected(context.Context, int64) error
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
	mux.HandleFunc("POST /api/v1/gate-access", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !currentUser(r).IsAdmin() {
			writeError(w, 403, "forbidden")
			return
		}
		h.adminCreate(w, r)
	})
	mux.HandleFunc("PUT /api/v1/gate-access/{id}", wrap(h.edit(false)))
	mux.HandleFunc("GET /api/v1/gate-access/{id}/events", wrap(h.events(false)))
	mux.HandleFunc("POST /api/v1/gate-access/{id}/decision", wrap(h.action(false)))
	mux.HandleFunc("DELETE /api/v1/gate-access/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !currentUser(r).IsAdmin() {
			writeError(w, 403, "forbidden")
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		if err := h.s.DeleteRejected(r.Context(), id); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h gateHandlers) adminCreate(w http.ResponseWriter, r *http.Request) {
	var in model.GateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	item, err := h.s.AdminCreate(r.Context(), currentUser(r), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, item)
}

func (h gateHandlers) registerResident(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/resident/gate-access", h.list(true))
	mux.HandleFunc("GET /api/v1/resident/gate-access/properties", h.properties)
	mux.HandleFunc("POST /api/v1/resident/gate-access", h.create)
	mux.HandleFunc("POST /api/v1/resident/gate-access/batch", h.createBatch)
	mux.HandleFunc("PUT /api/v1/resident/gate-access/{id}", h.edit(true))
	mux.HandleFunc("POST /api/v1/resident/gate-access/{id}/action", h.action(true))
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
		p, ok := parseListSpec(w, r, listSpec{Ints: []string{"premises_id"}, Texts: []string{"status", "display_status", "zone", "q", "recipient", "phone", "vehicle", "premises"}, Bools: []string{"include_rejected"}})
		if !ok {
			return
		}
		includeRejected := p.Bool("include_rejected")
		f := model.GateFilter{Status: p.texts["status"], DisplayStatus: p.texts["display_status"], Zone: p.texts["zone"], Q: p.texts["q"], Recipient: p.texts["recipient"], Phone: p.texts["phone"], Vehicle: p.texts["vehicle"], Premises: p.texts["premises"], PremisesID: p.ints["premises_id"], IncludeRejected: includeRejected != nil && *includeRejected}
		if !slices.Contains([]string{"", "pending", "approved", "revoke_requested", "rejected"}, f.DisplayStatus) || p.Deleted || !slices.Contains([]string{"", "pending", "approved", "rejected", "cancelled", "revoke_requested", "revoked", "expired", "ineligible"}, f.Status) || !slices.Contains([]string{"", "courtyard", "parking"}, f.Zone) || len([]rune(f.Q)) > 120 {
			writeError(w, 422, "gate: invalid filters")
			return
		}
		items, err := h.s.List(r.Context(), person, f, p.Limit, p.Offset)
		if err != nil {
			writeErr(w, err)
			return
		}
		if resident {
			for i := range items {
				items[i].Comment = ""
				items[i].ReviewNote = ""
				items[i].ELDESLabel = ""
			}
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
	item.Comment = ""
	item.ReviewNote = ""
	item.ELDESLabel = ""
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
		if resident {
			item.Comment = ""
			item.ReviewNote = ""
			item.ELDESLabel = ""
		}
		writeJSON(w, 200, item)
	}
}

func (h gateHandlers) edit(resident bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		person, ok := gatePerson(w, r, resident)
		if !ok {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in model.GateEditInput
		if !decodeJSON(w, r, &in) {
			return
		}
		in.Normalize()
		if err := in.Validate(); err != nil {
			writeErr(w, err)
			return
		}
		item, err := h.s.Edit(r.Context(), person, currentUser(r), id, in)
		if err != nil {
			writeErr(w, err)
			return
		}
		if resident {
			item.Comment = ""
			item.ReviewNote = ""
			item.ELDESLabel = ""
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
	for i := range items {
		items[i].Comment = ""
		items[i].ReviewNote = ""
	}
	writeJSON(w, 201, map[string]any{"items": items})
}
