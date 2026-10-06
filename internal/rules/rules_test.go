package rules

import (
	"testing"
	"time"

	"dom-backend/internal/model"
)

func ptr[T any](v T) *T { return &v }

// Синтетический дом: квартиры 1 и 2, машиноместа 7 и 8; у каждого помещения счёт ЖКУ и счёт капремонта.
func testIndex() *Index {
	idx := NewIndex()
	accID := int64(100)
	add := func(id int64, kind, number string) {
		idx.AddPremises(Premises{ID: id, Kind: kind, Number: number, BuildingID: 1})
		for _, purpose := range []string{"utilities", "capital_repair"} {
			accID++
			num := "0000" + map[string]string{"utilities": "00", "capital_repair": "50"}[purpose] + number
			idx.AddAccount(Account{ID: accID, Number: num + "x", PremisesID: id, Purpose: purpose})
		}
	}
	add(1, "apartment", "1")
	add(2, "apartment", "2")
	add(7, "parking_space", "7")
	add(8, "parking_space", "8")
	// ЖКУ квартиры 1 — счёт 101, капремонт — 102 и т.д.
	idx.People = []Person{
		{Tokens: Tokens("Иванов Иван Иванович"), Holders: []Holder{{PremisesID: 1}}},
		{Tokens: Tokens("Петрова Ёлка Сергеевна"), Holders: []Holder{{PremisesID: 2}, {PremisesID: 7}, {PremisesID: 8}}},
		{Tokens: Tokens("Копылова Светлана Геннадьевна"), Holders: []Holder{{PremisesID: 2}}},
	}
	idx.EntitiesByINN["7734401489"] = []Holder{{PremisesID: 2}}
	return idx
}

func rule(id int64, mode string, conds []model.RuleCondition, action model.RuleAction) model.PaymentRule {
	return model.PaymentRule{Meta: model.Meta{ID: id}, Name: "r", Enabled: true, Direction: "incoming", MatchMode: mode, Conditions: conds, Action: action}
}

func resolve(t *testing.T, rs []model.PaymentRule, p Payment) Outcome {
	t.Helper()
	e, errs := New(rs, testIndex())
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	p.Date = time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	return e.Resolve(p)
}

func TestConditions(t *testing.T) {
	c := func(field, op string, values ...string) []model.RuleCondition {
		return []model.RuleCondition{{Field: field, Op: op, Values: values}}
	}
	cat := model.RuleAction{Type: model.ActionSetCategory, CategoryID: ptr(int64(9))}
	p := Payment{PayerName: "ООО «Ромашка»", Purpose: "Оплата за капитальный ремонт по счёту 12", Amount: 1500.5, PayerINN: "7734401489", BankAccountID: 3}
	for name, tc := range map[string]struct {
		conds []model.RuleCondition
		mode  string
		want  bool
	}{
		"contains, case-insensitive":    {c("purpose", "contains", "КАПИТАЛЬНЫЙ ремонт"), "all", true},
		"contains any of values":        {c("purpose", "contains", "аренда", "ремонт"), "all", true},
		"not contains":                  {c("purpose", "not_contains", "аренда"), "all", true},
		"not contains fails":            {c("purpose", "not_contains", "ремонт"), "all", false},
		"equals normalised":             {c("payer_name", "equals", "ооо «ромашка»"), "all", true},
		"starts with":                   {c("payer_name", "starts_with", "ооо"), "all", true},
		"regex":                         {c("purpose", "regex", `счёту\s+\d+`), "all", true},
		"amount between":                {c("amount", "between", "1000", "2000"), "all", true},
		"amount gt":                     {c("amount", "gt", "1500,50"), "all", false},
		"amount equals":                 {c("amount", "equals", "1500.50"), "all", true},
		"bank account":                  {c("bank_account_id", "equals", "3"), "all", true},
		"inn":                           {c("payer_inn", "equals", "7734401489"), "all", true},
		"all of two, one fails":         {append(c("payer_inn", "equals", "7734401489"), c("purpose", "contains", "аренда")...), "all", false},
		"any of two, one holds":         {append(c("payer_inn", "equals", "000"), c("purpose", "contains", "ремонт")...), "any", true},
		"no conditions = every payment": {nil, "all", true},
	} {
		got := resolve(t, []model.PaymentRule{rule(1, tc.mode, tc.conds, cat)}, p)
		if got.Resolved() != tc.want {
			t.Errorf("%s: resolved = %v, want %v (%+v)", name, got.Resolved(), tc.want, got)
		}
	}
}

func TestIgnoreSpaces(t *testing.T) {
	cond := []model.RuleCondition{{Field: "purpose", Op: "contains", Values: []string{"ЛС 0000500107"}, IgnoreSpaces: true}}
	cat := model.RuleAction{Type: model.ActionSetCategory, CategoryID: ptr(int64(9))}
	if !resolve(t, []model.PaymentRule{rule(1, "all", cond, cat)}, Payment{Purpose: "ЛС 0 0 0 0 5 0 0 1 0 7"}).Resolved() {
		t.Error("spaced digits must match with ignore_spaces")
	}
	cond[0].IgnoreSpaces = false
	if resolve(t, []model.PaymentRule{rule(1, "all", cond, cat)}, Payment{Purpose: "ЛС 0 0 0 0 5 0 0 1 0 7"}).Resolved() {
		t.Error("spaced digits must not match without ignore_spaces")
	}
}

func TestActionAccountFromText(t *testing.T) {
	r := rule(1, "all", nil, model.RuleAction{Type: model.ActionAccountFromText, Pattern: `(?:^|\D)(0000\d{6})(?:\D|$)`})
	idx := testIndex()
	idx.AddAccount(Account{ID: 555, Number: "0000001101", PremisesID: 1, Purpose: "utilities"})
	e, _ := New([]model.PaymentRule{r}, idx)
	got := e.Resolve(Payment{Purpose: "ЛСИ0000001101,12.2025"})
	if !got.Resolved() || *got.AccountID != 555 {
		t.Errorf("account from text: %+v", got)
	}
	if got := e.Resolve(Payment{Purpose: "ЛС 0000009999"}); got.Resolved() || got.Reason == "" {
		t.Errorf("unknown number must not resolve: %+v", got)
	}
	if got := e.Resolve(Payment{Purpose: "без номера"}); got.Resolved() {
		t.Errorf("no number must not resolve: %+v", got)
	}
}

func TestActionLinkPremisesPicksAccountByBankType(t *testing.T) {
	r := rule(1, "all", nil, model.RuleAction{Type: model.ActionLinkPremises, PremisesID: ptr(int64(2))})
	ordinary := resolve(t, []model.PaymentRule{r}, Payment{})
	special := resolve(t, []model.PaymentRule{r}, Payment{BankSpecial: true})
	if !ordinary.Resolved() || *ordinary.AccountID != 103 || !special.Resolved() || *special.AccountID != 104 {
		t.Errorf("ordinary = %+v, special = %+v", ordinary, special)
	}
}

func TestActionPremisesFromText(t *testing.T) {
	r := rule(1, "all", nil, model.RuleAction{Type: model.ActionPremisesFromText, PremisesKind: "parking_space", Pattern: `м/м\s*(\d+)`})
	got := resolve(t, []model.PaymentRule{r}, Payment{Purpose: "Коммунальные платежи, м/м 8"})
	if !got.Resolved() || *got.AccountID != 107 { // машиноместо 8: ЖКУ 107, капремонт 108
		t.Errorf("parking from text: %+v", got)
	}
	if got := resolve(t, []model.PaymentRule{r}, Payment{Purpose: "м/м 99"}); got.Resolved() {
		t.Errorf("unknown parking: %+v", got)
	}
}

func TestLinkByOwner(t *testing.T) {
	owner := rule(1, "all", nil, model.RuleAction{Type: model.ActionLinkByOwner})
	rs := []model.PaymentRule{owner}
	for name, tc := range map[string]struct {
		p       Payment
		want    int64 // 0 — не определён
		special bool
	}{
		"exact name, one flat":          {Payment{PayerName: "ИВАНОВ ИВАН ИВАНОВИЧ"}, 101, false},
		"special account -> capital":    {Payment{PayerName: "Иванов Иван Иванович", BankSpecial: true}, 102, true},
		"word order and case":           {Payment{PayerName: "иван иванович ИВАНОВ"}, 101, false},
		"ё in the owner's name":         {Payment{PayerName: "ПЕТРОВА ЕЛКА СЕРГЕЕВНА", Purpose: "кв 2"}, 103, false},
		"truncated patronymic":          {Payment{PayerName: "КОПЫЛОВА СВЕТЛАНА ГЕННАДЬ"}, 103, false},
		"several premises, flat hint":   {Payment{PayerName: "Петрова Елка Сергеевна", Purpose: "Квартира №2"}, 103, false},
		"several premises, parking":     {Payment{PayerName: "Петрова Елка Сергеевна", Purpose: "ММ8 коммунальные"}, 107, false},
		"several premises, no hint":     {Payment{PayerName: "Петрова Елка Сергеевна", Purpose: "коммунальные платежи"}, 0, false},
		"hint for someone else's flat":  {Payment{PayerName: "Иванов Иван Иванович", Purpose: "кв. 2"}, 0, false},
		"unknown payer":                 {Payment{PayerName: "Сидоров Сидор Сидорович"}, 0, false},
		"one word is not a name":        {Payment{PayerName: "Иванов"}, 0, false},
		"company by INN":                {Payment{PayerName: "ООО Ромашка", PayerINN: "7734401489"}, 103, false},
		"contract number is not a flat": {Payment{PayerName: "Иванов Иван Иванович", Purpose: "по договору № 89 от 10.01.2024"}, 101, false},
	} {
		got := resolve(t, rs, tc.p)
		switch {
		case tc.want == 0 && got.Resolved():
			t.Errorf("%s: must not resolve, got %+v", name, got)
		case tc.want == 0 && got.Reason == "":
			t.Errorf("%s: unresolved payment needs a reason", name)
		case tc.want != 0 && (!got.Resolved() || *got.AccountID != tc.want):
			t.Errorf("%s: got %+v, want account %d", name, got, tc.want)
		}
	}
}

func TestFirstMatchWinsAndUnresolvedContinues(t *testing.T) {
	cat := model.RuleAction{Type: model.ActionSetCategory, CategoryID: ptr(int64(9))}
	byOwner := model.RuleAction{Type: model.ActionLinkByOwner}
	rs := []model.PaymentRule{
		rule(1, "all", []model.RuleCondition{{Field: "purpose", Op: "contains", Values: []string{"субсидия"}}}, cat),
		rule(2, "all", nil, byOwner),
		rule(3, "all", nil, cat),
	}
	if got := resolve(t, rs, Payment{PayerName: "Иванов Иван Иванович", Purpose: "субсидия"}); got.RuleID != 1 || got.CategoryID == nil {
		t.Errorf("first matching rule wins: %+v", got)
	}
	if got := resolve(t, rs, Payment{PayerName: "Иванов Иван Иванович"}); got.RuleID != 2 {
		t.Errorf("owner rule: %+v", got)
	}
	// владелец не найден: правило 2 не определило платёж, обработка идёт дальше к правилу 3
	if got := resolve(t, rs, Payment{PayerName: "Неизвестный Человек"}); got.RuleID != 3 {
		t.Errorf("unresolved rule must not stop processing: %+v", got)
	}
	// у платежа без определения причина берётся из содержательного правила, а не из «в тексте не найдено»
	only := []model.PaymentRule{
		rule(1, "all", nil, model.RuleAction{Type: model.ActionAccountFromText, Pattern: `(0000\d{6})`}),
		rule(2, "all", nil, byOwner),
	}
	got := resolve(t, only, Payment{PayerName: "Петрова Елка Сергеевна"})
	if got.Resolved() || got.Reason != "у плательщика несколько помещений, в назначении нет номера квартиры или машиноместа" {
		t.Errorf("reason = %q", got.Reason)
	}
}

func TestDisabledRulesAreSkipped(t *testing.T) {
	r := rule(1, "all", nil, model.RuleAction{Type: model.ActionSetCategory, CategoryID: ptr(int64(9))})
	r.Enabled = false
	if resolve(t, []model.PaymentRule{r}, Payment{}).Resolved() {
		t.Error("disabled rule must not apply")
	}
}

func TestNamesMatch(t *testing.T) {
	for _, tc := range []struct {
		payer, owner string
		want         bool
	}{
		{"Иванов Иван", "Иван Иванов", true},
		{"Иванов Иван", "Иванов Иван Иванович", false},
		{"Иванов Иван Ив", "Иванов Иван Иванович", true},
		{"Иванов Петр Иванович", "Иванов Иван Иванович", false},
		{"Иванов", "Иванов", false},
	} {
		if got := NamesMatch(Tokens(tc.payer), Tokens(tc.owner)); got != tc.want {
			t.Errorf("NamesMatch(%q, %q) = %v, want %v", tc.payer, tc.owner, got, tc.want)
		}
	}
}
