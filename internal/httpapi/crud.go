package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"dom-backend/internal/model"
)

// crudStore — хранилище сущности T с фильтром F: стандартный набор операций.
type crudStore[T any, F any] interface {
	List(ctx context.Context, f F, limit, offset int) ([]T, error)
	Get(ctx context.Context, id int64) (T, error)
	Create(ctx context.Context, in T) (T, error)
	Update(ctx context.Context, id int64, in T) (T, error)
	Delete(ctx context.Context, id int64) error
	Restore(ctx context.Context, id int64) (T, error)
}

// validator — сущность, умеющая проверять себя (model.*.Validate()).
type validator interface{ Validate() error }

// crudHandlers — стандартные маршруты ресурса: список, создание, получение, замена, удаление, восстановление.
// Для новых ресурсов вместо шести одинаковых методов на сущность.
type crudHandlers[T validator, F any] struct {
	path     string // например "/api/v1/bank-accounts"
	store    crudStore[T, F]
	filter   func(w http.ResponseWriter, r *http.Request) (F, listParams, bool)
	prepare  func(*T) // необязательно: значения по умолчанию и нормализация до Validate()
	readOnly bool     // только чтение: список и получение
}

func (h crudHandlers[T, F]) register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+h.path, h.list)
	mux.HandleFunc("GET "+h.path+"/{id}", h.get)
	if h.readOnly {
		return
	}
	mux.HandleFunc("POST "+h.path, h.create)
	mux.HandleFunc("PUT "+h.path+"/{id}", h.update)
	mux.HandleFunc("DELETE "+h.path+"/{id}", h.delete)
	mux.HandleFunc("POST "+h.path+"/{id}/restore", h.restore)
}

func (h crudHandlers[T, F]) list(w http.ResponseWriter, r *http.Request) {
	f, p, ok := h.filter(w, r)
	if !ok {
		return
	}
	items, err := h.store.List(r.Context(), f, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h crudHandlers[T, F]) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h crudHandlers[T, F]) decode(w http.ResponseWriter, r *http.Request) (T, bool) {
	var in T
	if !decodeJSON(w, r, &in) {
		return in, false
	}
	if h.prepare != nil {
		h.prepare(&in)
	}
	if err := in.Validate(); err != nil {
		writeErr(w, err)
		return in, false
	}
	return in, true
}

func (h crudHandlers[T, F]) create(w http.ResponseWriter, r *http.Request) {
	in, ok := h.decode(w, r)
	if !ok {
		return
	}
	item, err := h.store.Create(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h crudHandlers[T, F]) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	in, ok := h.decode(w, r)
	if !ok {
		return
	}
	item, err := h.store.Update(r.Context(), id, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h crudHandlers[T, F]) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h crudHandlers[T, F]) restore(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := h.store.Restore(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// Фильтры списков новых ресурсов.

func bankAccountFilter(w http.ResponseWriter, r *http.Request) (model.BankAccountFilter, listParams, bool) {
	p, ok := parseListSpec(w, r, listSpec{Ints: []string{"organization_id"}, Bools: []string{"is_special", "active"}})
	if !ok {
		return model.BankAccountFilter{}, p, false
	}
	return model.BankAccountFilter{Deleted: p.Deleted, OrganizationID: p.Int("organization_id"), IsSpecial: p.Bool("is_special"), Active: p.Bool("active")}, p, true
}

func paymentCategoryFilter(w http.ResponseWriter, r *http.Request) (model.PaymentCategoryFilter, listParams, bool) {
	p, ok := parseListSpec(w, r, listSpec{Texts: []string{"direction"}})
	if !ok {
		return model.PaymentCategoryFilter{}, p, false
	}
	return model.PaymentCategoryFilter{Deleted: p.Deleted, Direction: p.Text("direction")}, p, true
}

func paymentRegistryFilter(w http.ResponseWriter, r *http.Request) (model.PaymentRegistryFilter, listParams, bool) {
	p, ok := parseListSpec(w, r, listSpec{Ints: []string{"bank_account_id"}, Texts: []string{"q"}, Dates: []string{"date_from", "date_to"}, Bools: []string{"no_payment"}})
	if !ok {
		return model.PaymentRegistryFilter{}, p, false
	}
	return model.PaymentRegistryFilter{Deleted: p.Deleted, BankAccountID: p.Int("bank_account_id"), DateFrom: p.Date("date_from"), DateTo: p.Date("date_to"), Q: p.Text("q"), NoPayment: isTrue(p.Bool("no_payment"))}, p, true
}

func incomingPaymentFilter(w http.ResponseWriter, r *http.Request) (model.IncomingPaymentFilter, listParams, bool) {
	p, ok := parseListSpec(w, r, listSpec{
		Ints:   []string{"bank_account_id", "registry_id", "statement_id", "personal_account_id", "category_id"},
		Texts:  []string{"q", "binding", "export"},
		Dates:  []string{"date_from", "date_to"},
		Floats: []string{"amount_from", "amount_to"},
		Bools:  []string{"unlinked", "hide_registry_details", "attention"},
	})
	if !ok {
		return model.IncomingPaymentFilter{}, p, false
	}
	unlinked := p.Bool("unlinked")
	accountLinked, bindingUnlinked := false, false
	if b := p.Text("binding"); b != nil {
		switch *b {
		case "account":
			accountLinked = true
		case "unlinked":
			bindingUnlinked = true
		default:
			writeError(w, http.StatusBadRequest, "invalid binding: want account or unlinked")
			return model.IncomingPaymentFilter{}, p, false
		}
	}
	if e := p.Text("export"); e != nil && *e != "pending" && *e != "done" {
		writeError(w, http.StatusBadRequest, "invalid export: want pending or done")
		return model.IncomingPaymentFilter{}, p, false
	}
	return model.IncomingPaymentFilter{
		Export:  p.Text("export"),
		Deleted: p.Deleted, BankAccountID: p.Int("bank_account_id"), RegistryID: p.Int("registry_id"),
		StatementID: p.Int("statement_id"), PersonalAccountID: p.Int("personal_account_id"), CategoryID: p.Int("category_id"),
		DateFrom: p.Date("date_from"), DateTo: p.Date("date_to"),
		AmountFrom: p.Float("amount_from"), AmountTo: p.Float("amount_to"),
		Q: p.Text("q"), Unlinked: (unlinked != nil && *unlinked) || bindingUnlinked, AccountLinked: accountLinked, Attention: isTrue(p.Bool("attention")), HideRegistryDetails: isTrue(p.Bool("hide_registry_details")),
	}, p, true
}

func outgoingPaymentFilter(w http.ResponseWriter, r *http.Request) (model.OutgoingPaymentFilter, listParams, bool) {
	p, ok := parseListSpec(w, r, listSpec{
		Ints:   []string{"bank_account_id", "category_id", "statement_id"},
		Texts:  []string{"q"},
		Dates:  []string{"date_from", "date_to"},
		Floats: []string{"amount_from", "amount_to"},
		Bools:  []string{"unlinked"},
	})
	if !ok {
		return model.OutgoingPaymentFilter{}, p, false
	}
	unlinked := p.Bool("unlinked")
	return model.OutgoingPaymentFilter{Unlinked: unlinked != nil && *unlinked,
		Deleted: p.Deleted, BankAccountID: p.Int("bank_account_id"), CategoryID: p.Int("category_id"), StatementID: p.Int("statement_id"),
		DateFrom: p.Date("date_from"), DateTo: p.Date("date_to"),
		AmountFrom: p.Float("amount_from"), AmountTo: p.Float("amount_to"), Q: p.Text("q"),
	}, p, true
}

func paymentRuleFilter(w http.ResponseWriter, r *http.Request) (model.PaymentRuleFilter, listParams, bool) {
	p, ok := parseListSpec(w, r, listSpec{Bools: []string{"enabled"}, Texts: []string{"direction"}})
	if !ok {
		return model.PaymentRuleFilter{}, p, false
	}
	dir := p.Text("direction")
	if dir != nil && !slices.Contains(model.Directions, *dir) {
		writeError(w, http.StatusUnprocessableEntity, "direction: must be one of: incoming, outgoing")
		return model.PaymentRuleFilter{}, p, false
	}
	return model.PaymentRuleFilter{Deleted: p.Deleted, Enabled: p.Bool("enabled"), Direction: dir}, p, true
}

func meterFilter(w http.ResponseWriter, r *http.Request) (model.MeterFilter, listParams, bool) {
	p, ok := parseListSpec(w, r, listSpec{Ints: []string{"building_id", "premises_id"}, Texts: []string{"scope", "resource", "q"}, Bools: []string{"active"}})
	if !ok {
		return model.MeterFilter{}, p, false
	}
	if s := p.Text("scope"); s != nil && *s != "common" && *s != "individual" {
		writeError(w, http.StatusBadRequest, "invalid scope: want common or individual")
		return model.MeterFilter{}, p, false
	}
	if res := p.Text("resource"); res != nil && !slices.Contains(model.MeterResources, *res) {
		writeError(w, http.StatusUnprocessableEntity, "resource: must be one of: "+strings.Join(model.MeterResources, ", "))
		return model.MeterFilter{}, p, false
	}
	return model.MeterFilter{Deleted: p.Deleted, Scope: p.Text("scope"), BuildingID: p.Int("building_id"), PremisesID: p.Int("premises_id"),
		Resource: p.Text("resource"), Active: p.Bool("active"), Q: p.Text("q")}, p, true
}

func meterReadingFilter(w http.ResponseWriter, r *http.Request) (model.MeterReadingFilter, listParams, bool) {
	p, ok := parseListSpec(w, r, listSpec{Ints: []string{"meter_id"}, Dates: []string{"date_from", "date_to"}})
	if !ok {
		return model.MeterReadingFilter{}, p, false
	}
	return model.MeterReadingFilter{Deleted: p.Deleted, MeterID: p.Int("meter_id"), DateFrom: p.Date("date_from"), DateTo: p.Date("date_to")}, p, true
}

func serviceFilter(w http.ResponseWriter, r *http.Request) (model.ServiceFilter, listParams, bool) {
	p, ok := parseListSpec(w, r, listSpec{Ints: []string{"organization_id"}, Texts: []string{"kind", "calc_method", "account_purpose", "q"}, Bools: []string{"active"}})
	if !ok {
		return model.ServiceFilter{}, p, false
	}
	if k := p.Text("kind"); k != nil && !slices.Contains(model.ServiceKinds, *k) {
		writeError(w, http.StatusUnprocessableEntity, "kind: must be one of: "+strings.Join(model.ServiceKinds, ", "))
		return model.ServiceFilter{}, p, false
	}
	if m := p.Text("calc_method"); m != nil && !slices.Contains(model.ServiceCalcMethods, *m) {
		writeError(w, http.StatusUnprocessableEntity, "calc_method: must be one of: "+strings.Join(model.ServiceCalcMethods, ", "))
		return model.ServiceFilter{}, p, false
	}
	if a := p.Text("account_purpose"); a != nil && !slices.Contains(model.AccountPurposes, *a) {
		writeError(w, http.StatusUnprocessableEntity, "account_purpose: must be one of: "+strings.Join(model.AccountPurposes, ", "))
		return model.ServiceFilter{}, p, false
	}
	return model.ServiceFilter{Deleted: p.Deleted, OrganizationID: p.Int("organization_id"), Kind: p.Text("kind"), CalcMethod: p.Text("calc_method"), AccountPurpose: p.Text("account_purpose"),
		Active: p.Bool("active"), Q: p.Text("q")}, p, true
}

func serviceNormFilter(w http.ResponseWriter, r *http.Request) (model.ServiceNormFilter, listParams, bool) {
	p, ok := parseListSpec(w, r, listSpec{Ints: []string{"service_id", "building_id"}})
	if !ok {
		return model.ServiceNormFilter{}, p, false
	}
	return model.ServiceNormFilter{Deleted: p.Deleted, ServiceID: p.Int("service_id"), BuildingID: p.Int("building_id")}, p, true
}

func serviceTariffFilter(w http.ResponseWriter, r *http.Request) (model.ServiceTariffFilter, listParams, bool) {
	p, ok := parseListSpec(w, r, listSpec{Ints: []string{"service_id"}})
	if !ok {
		return model.ServiceTariffFilter{}, p, false
	}
	return model.ServiceTariffFilter{Deleted: p.Deleted, ServiceID: p.Int("service_id")}, p, true
}

func accountServiceFilter(w http.ResponseWriter, r *http.Request) (model.AccountServiceFilter, listParams, bool) {
	p, ok := parseListSpec(w, r, listSpec{Ints: []string{"account_id", "service_id", "premises_id", "building_id"}, Dates: []string{"active_on"}})
	if !ok {
		return model.AccountServiceFilter{}, p, false
	}
	return model.AccountServiceFilter{Deleted: p.Deleted, AccountID: p.Int("account_id"), ServiceID: p.Int("service_id"),
		PremisesID: p.Int("premises_id"), BuildingID: p.Int("building_id"), ActiveOn: p.Date("active_on")}, p, true
}

func isTrue(b *bool) bool { return b != nil && *b }
