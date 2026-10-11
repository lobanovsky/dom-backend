package store

import (
	"context"
	"testing"

	"dom-backend/internal/model"
)

func TestCounterpartiesFromPayments(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "cp-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	bank, err := NewBankAccounts(pool).Create(ctx, model.BankAccount{OrganizationID: org.ID, Number: "40703810000000009944", ValidFrom: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "bank_accounts", bank.ID)
	t.Cleanup(func() {
		for _, q := range []string{`UPDATE payment_registries SET payment_id = NULL WHERE bank_account_id = $1`, `DELETE FROM incoming_payments WHERE bank_account_id = $1`,
			`DELETE FROM outgoing_payments WHERE bank_account_id = $1`, `DELETE FROM payment_registries WHERE bank_account_id = $1`} {
			if _, err := pool.Exec(ctx, q, bank.ID); err != nil {
				t.Error(err)
			}
		}
	})

	out := func(name, inn string, day int, amount float64) {
		t.Helper()
		if _, err := NewOutgoingPayments(pool).Create(ctx, model.OutgoingPayment{BankAccountID: bank.ID, PaymentDate: date(2026, 1, day), Amount: amount, RecipientName: name, RecipientINN: &inn}); err != nil {
			t.Fatal(err)
		}
	}
	in := func(name, inn string, day int, amount float64) {
		t.Helper()
		if _, err := NewIncomingPayments(pool).Create(ctx, model.IncomingPayment{BankAccountID: bank.ID, PaymentDate: date(2026, 1, day), Amount: amount, PayerName: name, PayerINN: &inn}); err != nil {
			t.Fatal(err)
		}
	}
	// одно юрлицо в разном написании — один контрагент; два физлица с ИНН банка — два контрагента
	out(`ООО "Лифт Сервис"`, "7701000001", 3, 100)
	out("ооо лифт  сервис", "7701000001", 5, 50.5)
	out("Иванов Иван", "7707083893", 4, 10)
	out("Петров Пётр", "7707083893", 6, 20)
	in(`ООО "Лифт Сервис"`, "7701000001", 7, 5) // тот же контрагент и среди плательщиков
	in("ТЕСТ ПЛАТЕЛЬЩИК", "7701000002", 8, 1)

	// строка реестра (жилец) в контрагенты не попадает
	reg, err := NewPaymentRegistries(pool).Import(ctx, bank.ID, "cp-1.txt", []byte("cp file"), &model.ParsedRegistry{
		Source: "sber", Payments: []model.RegistryPayment{{Line: 1, Date: date(2026, 1, 9), Time: "10:00:00", ExternalID: "cp-x", AccountNum: "none", PayerName: "ЖИЛЕЦ РЕЕСТРА", Amount: 100, Raw: "raw"}}, TotalAmount: 100, TotalTransferred: 100})
	if err != nil || reg.Created != 1 {
		t.Fatalf("registry: %+v, err = %v", reg, err)
	}

	cps := NewCounterparties(pool)
	all, err := cps.List(ctx, model.CounterpartyFilter{Q: ptr("")}, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]model.Counterparty{}
	for _, c := range all {
		byName[c.Name] = c
	}
	var lift model.Counterparty
	for _, c := range all {
		if c.INN != nil && *c.INN == "7701000001" {
			lift = c
		}
	}
	if lift.OutgoingCount != 2 || lift.OutgoingTotal != 150.5 || lift.IncomingCount != 1 || lift.IncomingTotal != 5 || lift.LastPayment.Format("2006-01-02") != "2026-01-07" || lift.FirstPayment.Format("2006-01-02") != "2026-01-03" {
		t.Errorf("merged counterparty: %+v", lift)
	}
	if _, ok := byName["Иванов Иван"]; !ok || byName["Петров Пётр"].OutgoingCount != 1 {
		t.Errorf("individuals with the bank INN must stay separate: %v", byName)
	}
	if _, ok := byName["ЖИЛЕЦ РЕЕСТРА"]; ok {
		t.Error("registry payers are not counterparties")
	}

	outgoing, err := cps.List(ctx, model.CounterpartyFilter{Direction: "outgoing"}, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	hasPayer := false
	for _, c := range outgoing {
		hasPayer = hasPayer || c.Name == "ТЕСТ ПЛАТЕЛЬЩИК"
	}
	if hasPayer {
		t.Error("outgoing filter must not return payers only")
	}
	if found, err := cps.List(ctx, model.CounterpartyFilter{Q: ptr("7701000002")}, 100, 0); err != nil || len(found) != 1 || found[0].Name != "ТЕСТ ПЛАТЕЛЬЩИК" || found[0].IncomingCount != 1 {
		t.Errorf("search by inn: %+v, err = %v", found, err)
	}
}
