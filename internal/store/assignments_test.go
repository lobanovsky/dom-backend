package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"dom-backend/internal/model"
)

func TestAssignmentsLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)

	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "asg-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	bank, err := NewBankAccounts(pool).Create(ctx, model.BankAccount{OrganizationID: org.ID, Number: "40703810000000009966", ValidFrom: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "bank_accounts", bank.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "asg-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	flat, err := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "1"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises", flat.ID)
	parking, err := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: "parking_space", Number: "7"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises", parking.ID)
	accounts := NewAccounts(pool)
	mk := func(number string, premises int64, purpose string) model.Account {
		a, err := accounts.Create(ctx, model.Account{Number: number, PremisesID: premises, Purpose: purpose, Status: "active", OpenedAt: date(2026, 1, 1)})
		if err != nil {
			t.Fatal(err)
		}
		cleanup(t, pool, "personal_accounts", a.ID)
		return a
	}
	accFlat, accKR, accParking := mk("asg-u1", flat.ID, "utilities"), mk("asg-k1", flat.ID, "capital_repair"), mk("asg-u7", parking.ID, "utilities")
	person, err := NewPersons(pool).Create(ctx, model.Person{LastName: "Асгтестов", FirstName: "Аркадий", MiddleName: ptr("Борисович"), Phones: []string{}, Emails: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "persons", person.ID)
	own, err := NewOwnerships(pool).Create(ctx, model.Ownership{PremisesID: flat.ID, PersonID: &person.ID, ShareNum: 1, ShareDen: 1, ValidFrom: date(2026, 10, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "ownerships", own.ID)
	cat, err := NewPaymentCategories(pool).Create(ctx, model.PaymentCategory{Name: "asg-test аренда", Direction: "incoming"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "payment_categories", cat.ID)

	rulesStore, asg, pays := NewPaymentRules(pool), NewAssignments(pool), NewIncomingPayments(pool)
	if _, err := rulesStore.Create(ctx, model.PaymentRule{Name: "bad", Enabled: true, Direction: "incoming", MatchMode: "all",
		Action: model.RuleAction{Type: model.ActionLinkPremises, PremisesID: ptr(int64(1 << 40))}}); err == nil {
		t.Error("a rule referencing a missing premises must be rejected")
	}
	ruleText, err := rulesStore.Create(ctx, model.PaymentRule{Name: "asg-test номер в тексте", Enabled: true, Direction: "incoming", MatchMode: "all",
		Action: model.RuleAction{Type: model.ActionAccountFromText, Pattern: `(asg-u7)`}})
	if err != nil {
		t.Fatal(err)
	}
	ruleCat, err := rulesStore.Create(ctx, model.PaymentRule{Name: "asg-test аренда", Enabled: true, Direction: "incoming", MatchMode: "all",
		Conditions: model.RuleConditions{{Field: "purpose", Op: "contains", Values: []string{"аренда оборудования"}}},
		Action:     model.RuleAction{Type: model.ActionSetCategory, CategoryID: &cat.ID}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM assignment_run_items WHERE payment_id IN (SELECT id FROM incoming_payments WHERE bank_account_id = $1)`,
			`DELETE FROM incoming_payments WHERE bank_account_id = $1`,
		} {
			if _, err := pool.Exec(ctx, q, bank.ID); err != nil {
				t.Error(err)
			}
		}
		for _, id := range []int64{ruleText.ID, ruleCat.ID} {
			if _, err := pool.Exec(ctx, `DELETE FROM payment_rules WHERE id = $1`, id); err != nil {
				t.Error(err)
			}
		}
		if _, err := pool.Exec(ctx, `DELETE FROM assignment_runs WHERE candidates >= 0 AND id NOT IN (SELECT DISTINCT run_id FROM incoming_payments WHERE run_id IS NOT NULL) AND filters->>'bank_account_id' = $1`, fmt.Sprint(bank.ID)); err != nil {
			t.Error(err)
		}
	})

	mkPay := func(payer, purpose string, extra func(*model.IncomingPayment)) model.IncomingPayment {
		p := model.IncomingPayment{BankAccountID: bank.ID, PaymentDate: date(2026, 1, 5), Amount: 100, PayerName: payer, Purpose: ptr(purpose)}
		if extra != nil {
			extra(&p)
		}
		out, err := pays.Create(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	pOwner := mkPay("АСГТЕСТОВ АРКАДИЙ БОРИСОВИЧ", "кв. 1 коммунальные", nil)
	pText := mkPay("Кто-то Другой", "оплата asg-u7", nil)
	pCat := mkPay("ООО Ромашка", "аренда оборудования за январь", nil)
	pNone := mkPay("Совсем Неизвестный", "без признаков", nil)
	pManual := mkPay("Ручной Платёж", "руками", func(p *model.IncomingPayment) { p.PersonalAccountID = &accParking.ID })
	if pManual.AssignedBy == nil || *pManual.AssignedBy != "manual" {
		t.Fatalf("manual create must mark assigned_by = manual: %+v", pManual.AssignedBy)
	}

	scope := model.AssignmentScope{BankAccountID: &bank.ID}
	req := model.AssignRequest{Mode: model.AssignUnassigned, Scope: scope}

	// предпросмотр ничего не пишет и не трогает платежи с привязкой
	prev, err := asg.Preview(ctx, req)
	if err != nil || prev.Candidates != 4 || prev.New != 3 || prev.Unresolved != 1 || len(prev.Samples) != 3 || len(prev.Unmatched) != 1 || prev.Unmatched[0].PaymentID != pNone.ID {
		t.Fatalf("preview: %+v, err = %v", prev, err)
	}
	if got, _ := pays.Get(ctx, pOwner.ID); got.AssignedBy != nil || got.PersonalAccountID != nil {
		t.Fatalf("preview must not write: %+v", got)
	}
	// проверка одного правила (черновика из формы)
	draft := &model.PaymentRule{Name: "черновик", Direction: "incoming", MatchMode: "all",
		Conditions: model.RuleConditions{{Field: "payer_name", Op: "contains", Values: []string{"ромашка"}}}, Action: model.RuleAction{Type: model.ActionSetCategory, CategoryID: &cat.ID}}
	if one, err := asg.Preview(ctx, model.AssignRequest{Mode: model.AssignUnassigned, Scope: scope, Rule: draft}); err != nil || one.New != 1 || one.Unresolved != 3 {
		t.Errorf("draft rule preview: %+v, err = %v", one, err)
	}

	res, err := asg.Apply(ctx, req)
	if err != nil || res.RunID == 0 || res.New != 3 || res.Unresolved != 1 {
		t.Fatalf("apply: %+v, err = %v", res, err)
	}
	run1 := res.RunID
	check := func(id int64, wantAcc, wantCat *int64, wantBy string, wantRun int64) {
		t.Helper()
		got, err := pays.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		eq := func(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
		by := ""
		if got.AssignedBy != nil {
			by = *got.AssignedBy
		}
		var run int64
		if got.RunID != nil {
			run = *got.RunID
		}
		if !eq(got.PersonalAccountID, wantAcc) || !eq(got.CategoryID, wantCat) || by != wantBy || run != wantRun {
			t.Errorf("payment %d: acc=%v cat=%v by=%q run=%d; want acc=%v cat=%v by=%q run=%d", id, got.PersonalAccountID, got.CategoryID, by, run, wantAcc, wantCat, wantBy, wantRun)
		}
	}
	check(pOwner.ID, &accFlat.ID, nil, "rule", run1)
	check(pText.ID, &accParking.ID, nil, "rule", run1)
	check(pCat.ID, nil, &cat.ID, "rule", run1)
	check(pNone.ID, nil, nil, "", 0)
	check(pManual.ID, &accParking.ID, nil, "manual", 0)
	if got, _ := pays.Get(ctx, pCat.ID); got.RuleName == nil || *got.RuleName != "asg-test аренда" || got.RuleID == nil || *got.RuleID != ruleCat.ID {
		t.Errorf("rule name must be exposed: %+v", got.RuleName)
	}

	// повторное применение: менять нечего, запуск не создаётся
	if again, err := asg.Apply(ctx, req); err != nil || again.RunID != 0 || again.New != 0 {
		t.Errorf("second apply: %+v, err = %v", again, err)
	}

	// правило отключили: пересчёт снимает привязку «аренды» (cleared), остальные без изменений
	disabled := ruleCat
	disabled.Enabled = false
	if _, err := rulesStore.Update(ctx, ruleCat.ID, disabled); err != nil {
		t.Fatal(err)
	}
	recompute := model.AssignRequest{Mode: model.AssignRecompute, Scope: scope}
	if p2, err := asg.Preview(ctx, recompute); err != nil || p2.Cleared != 1 || p2.Same != 2 || p2.New != 0 {
		t.Fatalf("recompute preview: %+v, err = %v", p2, err)
	}
	res2, err := asg.Apply(ctx, recompute)
	if err != nil || res2.RunID == 0 || res2.Cleared != 1 {
		t.Fatalf("recompute apply: %+v, err = %v", res2, err)
	}
	run2 := res2.RunID
	check(pCat.ID, nil, nil, "", run2)

	// ручное изменение после запуска снимает связь с запуском
	edit := pOwner
	edit.PersonalAccountID = &accKR.ID
	edit.PaymentDate = date(2026, 1, 5)
	if _, err := pays.Update(ctx, pOwner.ID, edit); err != nil {
		t.Fatal(err)
	}
	check(pOwner.ID, &accKR.ID, nil, "manual", 0)

	// откат запуска 1: восстанавливается только то, что всё ещё от этого запуска (pText); pOwner изменён вручную, pCat — запуском 2
	rb, err := asg.Rollback(ctx, run1)
	if err != nil || rb.Restored != 1 || rb.Kept != 2 {
		t.Fatalf("rollback run 1: %+v, err = %v", rb, err)
	}
	check(pText.ID, nil, nil, "", 0)
	check(pOwner.ID, &accKR.ID, nil, "manual", 0)
	var se *Error
	if _, err := asg.Rollback(ctx, run1); !errors.As(err, &se) || !errors.Is(se.Kind, ErrConflict) {
		t.Errorf("second rollback: err = %v, want conflict", err)
	}
	// откат запуска 2 возвращает платёж к состоянию после запуска 1
	if rb2, err := asg.Rollback(ctx, run2); err != nil || rb2.Restored != 1 {
		t.Fatalf("rollback run 2: %+v, err = %v", rb2, err)
	}
	check(pCat.ID, nil, &cat.ID, "rule", run1)

	runs, err := asg.Runs(ctx, "incoming", 50, 0)
	if err != nil || len(runs) < 2 || runs[0].RolledBackAt == nil || runs[0].ID != run2 {
		t.Errorf("history: %+v, err = %v", runs, err)
	}
	if _, err := asg.Rollback(ctx, 1<<40); !errors.As(err, &se) || !errors.Is(se.Kind, ErrNotFound) {
		t.Errorf("unknown run: err = %v", err)
	}

	// порядок правил
	if err := rulesStore.Reorder(ctx, []int64{ruleCat.ID, ruleText.ID}); err != nil {
		t.Fatal(err)
	}
	if list, err := rulesStore.List(ctx, model.PaymentRuleFilter{}, 100, 0); err != nil || list[0].ID != ruleCat.ID || list[1].ID != ruleText.ID {
		t.Errorf("reorder: %+v, err = %v", list, err)
	}
}

// Исходящие платежи: правила ставят только категорию, запуск пишет направление, откат работает по своей таблице.
func TestOutgoingAssignments(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)

	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "asg-out-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	bank, err := NewBankAccounts(pool).Create(ctx, model.BankAccount{OrganizationID: org.ID, Number: "40703810000000009977", ValidFrom: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "bank_accounts", bank.ID)
	cats := NewPaymentCategories(pool)
	taxes, err := cats.Create(ctx, model.PaymentCategory{Name: "asg-out налоги", Direction: "outgoing"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "payment_categories", taxes.ID)
	inCat, err := cats.Create(ctx, model.PaymentCategory{Name: "asg-out входящая", Direction: "incoming"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "payment_categories", inCat.ID)

	rulesStore, asg, pays := NewPaymentRules(pool), NewAssignments(pool), NewOutgoingPayments(pool)
	// категория другого направления не принимается
	if _, err := rulesStore.Create(ctx, model.PaymentRule{Name: "bad", Enabled: true, Direction: "outgoing", MatchMode: "all",
		Action: model.RuleAction{Type: model.ActionSetCategory, CategoryID: &inCat.ID}}); err == nil {
		t.Error("an outgoing rule must not accept an incoming category")
	}
	rule, err := rulesStore.Create(ctx, model.PaymentRule{Name: "asg-out ЕНП", Enabled: true, Direction: "outgoing", MatchMode: "all",
		Conditions: model.RuleConditions{{Field: "recipient_inn", Op: "equals", Values: []string{"7727406020"}}},
		Action:     model.RuleAction{Type: model.ActionSetCategory, CategoryID: &taxes.ID}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM assignment_run_items WHERE run_id IN (SELECT id FROM assignment_runs WHERE filters->>'bank_account_id' = $1)`,
			`DELETE FROM outgoing_payments WHERE bank_account_id = $1`,
			`DELETE FROM assignment_runs WHERE filters->>'bank_account_id' = $1`,
		} {
			if _, err := pool.Exec(ctx, q, fmt.Sprint(bank.ID)); err != nil {
				t.Error(err)
			}
		}
		if _, err := pool.Exec(ctx, `DELETE FROM payment_rules WHERE id = $1`, rule.ID); err != nil {
			t.Error(err)
		}
	})
	// правило исходящих не мешает входящим и наоборот
	if list, err := rulesStore.List(ctx, model.PaymentRuleFilter{Direction: ptr("incoming")}, 100, 0); err != nil {
		t.Fatal(err)
	} else {
		for _, r := range list {
			if r.ID == rule.ID {
				t.Error("an outgoing rule must not be listed among incoming ones")
			}
		}
	}

	mkPay := func(name, inn, purpose string, cat *int64) model.OutgoingPayment {
		out, err := pays.Create(ctx, model.OutgoingPayment{BankAccountID: bank.ID, PaymentDate: date(2026, 2, 5), Amount: 100,
			RecipientName: name, RecipientINN: ptr(inn), Purpose: ptr(purpose), CategoryID: cat})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	pTax := mkPay("УФК по г. Москве", "7727406020", "ЕНП", nil)
	pOther := mkPay("ООО Ромашка", "7700000001", "за услуги", nil)
	pManual := mkPay("УФК по г. Москве", "7727406020", "руками", &taxes.ID)
	if pManual.AssignedBy == nil || *pManual.AssignedBy != "manual" {
		t.Fatalf("manual create must mark assigned_by = manual: %v", pManual.AssignedBy)
	}

	req := model.AssignRequest{Direction: "outgoing", Mode: model.AssignUnassigned, Scope: model.AssignmentScope{BankAccountID: &bank.ID}}
	prev, err := asg.Preview(ctx, req)
	if err != nil || prev.Candidates != 2 || prev.New != 1 || prev.Unresolved != 1 || prev.Unmatched[0].PaymentID != pOther.ID {
		t.Fatalf("preview: %+v, err = %v", prev, err)
	}
	if prev.Samples[0].CategoryName != taxes.Name {
		t.Errorf("sample must name the category: %+v", prev.Samples[0])
	}
	// правило входящих направлений к исходящим не применяется
	if in, err := asg.Preview(ctx, model.AssignRequest{Mode: model.AssignUnassigned, Scope: model.AssignmentScope{BankAccountID: &bank.ID}}); err != nil || in.Candidates != 0 {
		t.Errorf("incoming preview must not see outgoing payments: %+v, err = %v", in, err)
	}

	res, err := asg.Apply(ctx, req)
	if err != nil || res.RunID == 0 || res.New != 1 {
		t.Fatalf("apply: %+v, err = %v", res, err)
	}
	got, _ := pays.Get(ctx, pTax.ID)
	if got.CategoryID == nil || *got.CategoryID != taxes.ID || got.AssignedBy == nil || *got.AssignedBy != "rule" || got.RuleName == nil || got.RunID == nil || *got.RunID != res.RunID {
		t.Fatalf("rule result: %+v", got)
	}
	if got, _ := pays.Get(ctx, pOther.ID); got.CategoryID != nil {
		t.Errorf("unmatched payment must stay unassigned: %+v", got)
	}
	if again, err := asg.Apply(ctx, req); err != nil || again.RunID != 0 {
		t.Errorf("second apply: %+v, err = %v", again, err)
	}
	if runs, err := asg.Runs(ctx, "outgoing", 50, 0); err != nil || len(runs) == 0 || runs[0].ID != res.RunID || runs[0].Direction != "outgoing" {
		t.Errorf("history: %+v, err = %v", runs, err)
	}

	// ручная смена категории снимает связь с запуском, и откат её не трогает
	edit := got
	edit.CategoryID = nil
	if _, err := pays.Update(ctx, pTax.ID, edit); err != nil {
		t.Fatal(err)
	}
	if rb, err := asg.Rollback(ctx, res.RunID); err != nil || rb.Restored != 0 || rb.Kept != 1 {
		t.Fatalf("rollback after manual edit: %+v, err = %v", rb, err)
	}

	// откат нетронутого запуска возвращает платёж к «без категории»
	res2, err := asg.Apply(ctx, req)
	if err != nil || res2.New != 1 {
		t.Fatalf("apply again: %+v, err = %v", res2, err)
	}
	if rb, err := asg.Rollback(ctx, res2.RunID); err != nil || rb.Restored != 1 {
		t.Fatalf("rollback: %+v, err = %v", rb, err)
	}
	if got, _ := pays.Get(ctx, pTax.ID); got.CategoryID != nil || got.AssignedBy != nil {
		t.Errorf("rollback must restore the previous state: %+v", got)
	}
}
