package store

import (
	"context"
	"testing"
	"time"

	"dom-backend/internal/model"
)

// Реестр находит платёж из выписки по имени файла в назначении и сумме — в любом порядке загрузки.
func TestRegistryLinksToStatementPayment(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "link-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	bank, err := NewBankAccounts(pool).Create(ctx, model.BankAccount{OrganizationID: org.ID, Number: "40703810000000009955", ValidFrom: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "bank_accounts", bank.ID)
	t.Cleanup(func() {
		for _, q := range []string{`UPDATE payment_registries SET payment_id = NULL WHERE bank_account_id = $1`, `DELETE FROM incoming_payments WHERE bank_account_id = $1`, `DELETE FROM payment_registries WHERE bank_account_id = $1`, `DELETE FROM incoming_payments WHERE bank_account_id = $1`, `DELETE FROM bank_statements WHERE bank_account_id = $1`} {
			if _, err := pool.Exec(ctx, q, bank.ID); err != nil {
				t.Error(err)
			}
		}
	})

	regs, stmts, payments := NewPaymentRegistries(pool), NewBankStatements(pool), NewIncomingPayments(pool)
	reg := func(name string, kop int64, ids ...string) *model.ParsedRegistry {
		r := &model.ParsedRegistry{Source: "vtb", RegistryDate: ptr(date(2026, 10, 8)), TotalAmount: kop, TotalTransferred: kop}
		for _, id := range ids {
			r.Payments = append(r.Payments, model.RegistryPayment{Line: 1, Date: date(2026, 10, 8), Time: "10:00:00", ExternalID: id, AccountNum: "no-account", PayerName: "ТЕСТ", Amount: kop / int64(len(ids)), Raw: "raw " + id})
		}
		return r
	}
	op := func(key, purpose string, kop int64) model.StatementOperation {
		return model.StatementOperation{Row: 1, At: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), Amount: kop, CounterName: "ФИЛИАЛ БАНКА ВТБ", DocNumber: key, Purpose: purpose, Raw: "raw " + key, DedupKey: key}
	}
	stmt := func(ops ...model.StatementOperation) *model.ParsedStatement {
		return &model.ParsedStatement{Account: bank.Number, CreditCount: len(ops), Operations: ops}
	}

	// 1. реестр раньше платежа: связи нет, подсказка в ответе пустая
	a, err := regs.Import(ctx, bank.ID, "081026_VTB_231068_002.txt", []byte("reg A"), reg("081026_VTB_231068_002.txt", 274092, "a1", "a2", "a3"))
	if err != nil || a.Payment != nil {
		t.Fatalf("registry before payment: %+v, err = %v", a, err)
	}
	// платёж пришёл (регистр имени другой, перед именем чужой текст): связь ставится в транзакции импорта выписки
	sA, err := stmts.Import(ctx, "s1.txt", []byte("stmt 1"), stmt(
		op("p1", "ПЕРЕВОД СРЕДСТВ //РЕЕСТР// 081026_vtb_231068_002.TXT,КОЛ-ВО-3", 274092),
		op("p2", "то же имя, но другая сумма 081026_VTB_231068_002.txt", 100),
	))
	if err != nil || sA.RegistriesLinked != 1 {
		t.Fatalf("statement import: %+v, err = %v", sA, err)
	}
	r, err := regs.Get(ctx, a.RegistryID)
	if err != nil || r.PaymentID == nil || r.PaymentAmount == nil || *r.PaymentAmount != 2740.92 || r.PaymentStatementID == nil || *r.PaymentStatementID != sA.StatementID {
		t.Fatalf("registry after statement: %+v, err = %v", r, err)
	}

	// 2. платёж раньше реестра: связь ставится при импорте реестра, в ответе есть платёж
	if _, err := stmts.Import(ctx, "s2.txt", []byte("stmt 2"), stmt(op("p3", "РЕЕСТР 091026_VTB_231068_002.txt", 50000))); err != nil {
		t.Fatal(err)
	}
	b, err := regs.Import(ctx, bank.ID, "091026_VTB_231068_002.txt", []byte("reg B"), reg("091026_VTB_231068_002.txt", 50000, "b1"))
	if err != nil || b.Payment == nil || b.Payment.Amount != 500 {
		t.Fatalf("payment before registry: %+v, err = %v", b, err)
	}

	// 3. сумма не сошлась — не связываем; повторное сопоставление ничего не меняет и объясняет причину
	c, err := regs.Import(ctx, bank.ID, "101026_VTB_231068_002.txt", []byte("reg C"), reg("101026_VTB_231068_002.txt", 70000, "c1"))
	if err != nil || c.Payment != nil {
		t.Fatalf("registry without payment: %+v, err = %v", c, err)
	}
	linked, unmatched, err := regs.Relink(ctx)
	if err != nil || linked != 0 {
		t.Fatalf("relink: linked = %d, err = %v", linked, err)
	}
	found := false
	for _, u := range unmatched {
		if u.RegistryID == c.RegistryID {
			found = u.Reason == UnmatchedNoPayment
		}
	}
	if !found {
		t.Errorf("unmatched = %+v, want registry %d with no_payment", unmatched, c.RegistryID)
	}

	// 4. платёж банка показывает свою детализацию, строки связанных реестров скрываются фильтром
	hidden := model.IncomingPaymentFilter{BankAccountID: &bank.ID, HideRegistryDetails: true}
	all, err := payments.List(ctx, model.IncomingPaymentFilter{BankAccountID: &bank.ID}, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	shown, err := payments.List(ctx, hidden, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	// всего: платежей банка 3 (p1, p2, p3) + строки реестров a1-a3, b1, c1; скрыть нужно a1-a3 и b1, строка c1 (платёж не найден) остаётся
	if len(all) != 8 || len(shown) != 4 {
		t.Errorf("all = %d, shown = %d, want 8 and 4", len(all), len(shown))
	}
	for _, p := range shown {
		if p.DetailRegistryID != nil && (p.DetailRegistryFileName == nil || p.DetailCount == nil) {
			t.Errorf("detail fields: %+v", p)
		}
		if p.RegistryID != nil && p.ExternalID != nil && *p.ExternalID != "c1" {
			t.Errorf("registry row %v must be hidden", *p.ExternalID)
		}
	}
	withDetail := 0
	for _, p := range all {
		if p.DetailRegistryID != nil && *p.DetailRegistryID == a.RegistryID && p.DetailCount != nil && *p.DetailCount == 3 {
			withDetail++
		}
	}
	if withDetail != 1 {
		t.Errorf("payment with the registry detail: %d, want 1", withDetail)
	}
	if list, err := regs.List(ctx, model.PaymentRegistryFilter{BankAccountID: &bank.ID, NoPayment: true}, 50, 0); err != nil || len(list) != 1 || list[0].ID != c.RegistryID {
		t.Errorf("registries without payment: %+v, err = %v", list, err)
	}

	// 5. два платежа с одинаковым именем и суммой — неоднозначно, не связываем
	if _, err := stmts.Import(ctx, "s3.txt", []byte("stmt 3"), stmt(op("p4", "081126_VTB_231068_001.txt", 900), op("p5", "081126_VTB_231068_001.txt", 900))); err != nil {
		t.Fatal(err)
	}
	d, err := regs.Import(ctx, bank.ID, "081126_VTB_231068_001.txt", []byte("reg D"), reg("081126_VTB_231068_001.txt", 900, "d1"))
	if err != nil || d.Payment != nil {
		t.Fatalf("ambiguous: %+v, err = %v", d, err)
	}
	if _, unmatched, _ := regs.Relink(ctx); len(unmatched) < 2 {
		t.Errorf("unmatched = %+v", unmatched)
	} else {
		for _, u := range unmatched {
			if u.RegistryID == d.RegistryID && u.Reason != UnmatchedAmbiguous {
				t.Errorf("reason = %q, want ambiguous", u.Reason)
			}
		}
	}
}
