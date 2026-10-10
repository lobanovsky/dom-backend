package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"dom-backend/internal/auth"
	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

type fakeAssignments struct {
	reqs    []model.AssignRequest
	order   []int64
	nothing bool

	runsDirection string
}

func (f *fakeAssignments) Preview(_ context.Context, r model.AssignRequest) (model.AssignPreview, error) {
	f.reqs = append(f.reqs, r)
	return model.AssignPreview{Candidates: 3, New: 2, Unresolved: 1}, nil
}

func (f *fakeAssignments) Apply(_ context.Context, r model.AssignRequest) (model.AssignResult, error) {
	f.reqs = append(f.reqs, r)
	if f.nothing {
		return model.AssignResult{Candidates: 1}, nil
	}
	return model.AssignResult{RunID: 4, Candidates: 3, New: 2}, nil
}

func (f *fakeAssignments) Rollback(_ context.Context, id int64) (model.RollbackResult, error) {
	switch id {
	case 404:
		return model.RollbackResult{}, &store.Error{Kind: store.ErrNotFound, Msg: "assignment run not found"}
	case 409:
		return model.RollbackResult{}, &store.Error{Kind: store.ErrConflict, Msg: "assignment run is already rolled back"}
	}
	return model.RollbackResult{Restored: 2, Kept: 1}, nil
}

func (f *fakeAssignments) Runs(_ context.Context, direction string, _, _ int) ([]model.AssignRun, error) {
	f.runsDirection = direction
	return []model.AssignRun{{ID: 4, Mode: "unassigned"}}, nil
}

func (f *fakeAssignments) Reorder(_ context.Context, ids []int64) error {
	f.order = ids
	return nil
}

func assignRouter(t *testing.T, f *fakeAssignments) (http.Handler, *http.Cookie) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	h := NewRouter(Deps{Auth: auth.New("admin", string(hash), "secret", time.Hour), Assignments: f, RuleOrder: f})
	return h, login(t, h)
}

func call(h http.Handler, c *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAssignmentEndpoints(t *testing.T) {
	f := &fakeAssignments{}
	h, c := assignRouter(t, f)

	rec := call(h, c, "POST", "/api/v1/payment-assignments/preview", `{"mode":"unassigned","scope":{"bank_account_id":3,"date_from":"2026-01-01","q":"иванов"}}`)
	var prev model.AssignPreview
	_ = json.Unmarshal(rec.Body.Bytes(), &prev)
	if rec.Code != http.StatusOK || prev.New != 2 || len(f.reqs) != 1 || *f.reqs[0].Scope.BankAccountID != 3 || f.reqs[0].Scope.DateFrom.Format("2006-01-02") != "2026-01-01" || *f.reqs[0].Scope.Q != "иванов" {
		t.Errorf("preview: %d %s, reqs = %+v", rec.Code, rec.Body.String(), f.reqs)
	}
	if rec := call(h, c, "POST", "/api/v1/payment-assignments/preview", `{"mode":"x","unknown":1}`); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: status = %d, want 400", rec.Code)
	}

	if rec := call(h, c, "POST", "/api/v1/payment-assignments", `{"mode":"recompute","scope":{}}`); rec.Code != http.StatusCreated {
		t.Errorf("apply: status = %d, want 201", rec.Code)
	}
	f.nothing = true
	if rec := call(h, c, "POST", "/api/v1/payment-assignments", `{"mode":"unassigned","scope":{}}`); rec.Code != http.StatusOK {
		t.Errorf("apply with nothing to change: status = %d, want 200", rec.Code)
	}

	for id, want := range map[string]int{"7": http.StatusOK, "404": http.StatusNotFound, "409": http.StatusConflict, "abc": http.StatusBadRequest} {
		if rec := call(h, c, "POST", "/api/v1/payment-assignments/"+id+"/rollback", ""); rec.Code != want {
			t.Errorf("rollback %s: status = %d, want %d", id, rec.Code, want)
		}
	}
	if rec := call(h, c, "GET", "/api/v1/payment-assignments", ""); rec.Code != http.StatusOK || f.runsDirection != "" {
		t.Errorf("runs: status = %d, direction = %q", rec.Code, f.runsDirection)
	}
	if rec := call(h, c, "GET", "/api/v1/payment-assignments?direction=outgoing", ""); rec.Code != http.StatusOK || f.runsDirection != "outgoing" {
		t.Errorf("runs of outgoing: status = %d, direction = %q", rec.Code, f.runsDirection)
	}
	if rec := call(h, c, "GET", "/api/v1/payment-assignments?direction=sideways", ""); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("runs with bad direction: status = %d, want 422", rec.Code)
	}
	if rec := call(h, c, "POST", "/api/v1/payment-assignments/preview", `{"direction":"outgoing","mode":"unassigned","scope":{}}`); rec.Code != http.StatusOK || f.reqs[len(f.reqs)-1].Direction != "outgoing" {
		t.Errorf("preview of outgoing: status = %d", rec.Code)
	}

	if rec := call(h, c, "POST", "/api/v1/payment-rules/reorder", `{"ids":[3,1,2]}`); rec.Code != http.StatusNoContent || len(f.order) != 3 || f.order[0] != 3 {
		t.Errorf("reorder: status = %d, order = %v", rec.Code, f.order)
	}
	if rec := call(h, c, "POST", "/api/v1/payment-rules/reorder", `{"ids":[]}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("reorder without ids: status = %d, want 422", rec.Code)
	}
}

func TestRuleValidation(t *testing.T) {
	good := model.PaymentRule{Name: "r", Direction: "incoming", MatchMode: "all", Action: model.RuleAction{Type: model.ActionLinkByOwner}}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid rule: %v", err)
	}
	for name, mutate := range map[string]func(*model.PaymentRule){
		"no name":        func(r *model.PaymentRule) { r.Name = " " },
		"bad match mode": func(r *model.PaymentRule) { r.MatchMode = "some" },
		"unknown field": func(r *model.PaymentRule) {
			r.Conditions = model.RuleConditions{{Field: "x", Op: "contains", Values: []string{"a"}}}
		},
		"bad op for text": func(r *model.PaymentRule) {
			r.Conditions = model.RuleConditions{{Field: "purpose", Op: "gt", Values: []string{"a"}}}
		},
		"empty values": func(r *model.PaymentRule) { r.Conditions = model.RuleConditions{{Field: "purpose", Op: "contains"}} },
		"bad regex": func(r *model.PaymentRule) {
			r.Conditions = model.RuleConditions{{Field: "purpose", Op: "regex", Values: []string{"("}}}
		},
		"between needs two": func(r *model.PaymentRule) {
			r.Conditions = model.RuleConditions{{Field: "amount", Op: "between", Values: []string{"1"}}}
		},
		"amount not a number": func(r *model.PaymentRule) {
			r.Conditions = model.RuleConditions{{Field: "amount", Op: "gt", Values: []string{"abc"}}}
		},
		"unknown action":      func(r *model.PaymentRule) { r.Action = model.RuleAction{Type: "explode"} },
		"link premises no id": func(r *model.PaymentRule) { r.Action = model.RuleAction{Type: model.ActionLinkPremises} },
		"pattern no group": func(r *model.PaymentRule) {
			r.Action = model.RuleAction{Type: model.ActionAccountFromText, Pattern: `\d+`}
		},
		"premises from text without kind": func(r *model.PaymentRule) {
			r.Action = model.RuleAction{Type: model.ActionPremisesFromText, Pattern: `(\d+)`}
		},
	} {
		r := good
		mutate(&r)
		if r.Validate() == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}

func TestOutgoingRuleValidation(t *testing.T) {
	cat := model.RuleAction{Type: model.ActionSetCategory, CategoryID: ptr(int64(5))}
	good := model.PaymentRule{Name: "r", Direction: "outgoing", MatchMode: "all", Action: cat,
		Conditions: model.RuleConditions{{Field: "recipient_inn", Op: "equals", Values: []string{"7727406020"}}}}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid outgoing rule: %v", err)
	}
	for name, mutate := range map[string]func(*model.PaymentRule){
		"payer field in outgoing": func(r *model.PaymentRule) {
			r.Conditions = model.RuleConditions{{Field: "payer_name", Op: "contains", Values: []string{"a"}}}
		},
		"link by owner": func(r *model.PaymentRule) { r.Action = model.RuleAction{Type: model.ActionLinkByOwner} },
		"link premises": func(r *model.PaymentRule) {
			r.Action = model.RuleAction{Type: model.ActionLinkPremises, PremisesID: ptr(int64(1))}
		},
		"unknown direction": func(r *model.PaymentRule) { r.Direction = "sideways" },
	} {
		r := good
		mutate(&r)
		if r.Validate() == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	// у входящих recipient_* недоступны
	in := good
	in.Direction = "incoming"
	if in.Validate() == nil {
		t.Error("recipient_inn must not be accepted in an incoming rule")
	}
}

func ptr[T any](v T) *T { return &v }
