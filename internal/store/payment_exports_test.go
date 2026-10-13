package store

import (
	"context"
	"errors"
	"testing"

	"dom-backend/internal/model"
	"dom-backend/internal/sberregistry"
)

func TestPaymentExports(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "export-test", INN: ptr("7701234567")})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "г Москва, ул Тестовая, 1"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	pr, err := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "9"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises", pr.ID)
	acc, err := NewAccounts(pool).Create(ctx, model.Account{Number: "0009991001", PremisesID: pr.ID, Purpose: "utilities", Status: "active", OpenedAt: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "personal_accounts", acc.ID)

	banks := NewBankAccounts(pool)
	bank, err := banks.Create(ctx, model.BankAccount{OrganizationID: org.ID, Number: "40703810000000009981", ValidFrom: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	bank2, err := banks.Create(ctx, model.BankAccount{OrganizationID: org.ID, Number: "40703810000000009982", ValidFrom: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	// Выгрузки ссылаются на платежи и счета: убрать их надо до удаления счетов (cleanup выполняются в обратном порядке).
	cleanup(t, pool, "bank_accounts", bank2.ID)
	cleanup(t, pool, "bank_accounts", bank.ID)
	t.Cleanup(func() {
		for _, id := range []int64{bank.ID, bank2.ID} {
			_, _ = pool.Exec(context.Background(), `DELETE FROM incoming_payments WHERE bank_account_id = $1`, id)
			_, _ = pool.Exec(context.Background(), `DELETE FROM payment_exports WHERE bank_account_id = $1`, id)
		}
	})

	in, exports := NewIncomingPayments(pool), NewPaymentExports(pool)
	mk := func(bankID int64, amount float64, name string, account *int64) model.IncomingPayment {
		p, err := in.Create(ctx, model.IncomingPayment{BankAccountID: bankID, PaymentDate: date(2026, 8, 3), Amount: amount, PayerName: name, PersonalAccountID: account})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p1 := mk(bank.ID, 100.50, "ИВАНОВ", &acc.ID)
	p2 := mk(bank.ID, 200, "ПЕТРОВ", &acc.ID)
	mk(bank.ID, 50, "БЕЗ ЛС", nil)
	mk(bank2.ID, 70, "ДРУГОЙ СЧЁТ", &acc.ID)

	onBank := model.IncomingPaymentFilter{BankAccountID: &bank.ID}
	pv, err := exports.Preview(ctx, onBank, ExportSelection{Mode: "filter"})
	if err != nil || pv.Count != 2 || pv.Total != 300.5 || len(pv.BankAccountIDs) != 1 {
		t.Fatalf("preview = %+v, err = %v", pv, err)
	}
	if _, err := exports.Create(ctx, model.IncomingPaymentFilter{}, ExportSelection{Mode: "ids", IDs: []int64{p1.ID}}); err != nil {
		// все платежи отбираются без фильтра по счёту только по id: один счёт — это допустимо
		t.Fatalf("create by ids: %v", err)
	}
	// p1 выгружен; теперь выбор «все, кроме исключённых» без фильтра по счёту видит платежи двух счетов
	if _, err := exports.Create(ctx, model.IncomingPaymentFilter{}, ExportSelection{Mode: "filter", IDs: []int64{}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("several bank accounts: err = %v, want invalid", err)
	}
	if _, err := exports.Create(ctx, onBank, ExportSelection{Mode: "filter", ExpectedCount: ptr(5)}); !errors.Is(err, ErrConflict) {
		t.Errorf("expected_count mismatch: err = %v, want conflict", err)
	}
	if _, err := exports.Create(ctx, onBank, ExportSelection{Mode: "filter", IDs: []int64{p2.ID}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("nothing left: err = %v, want invalid", err)
	}

	got, err := in.Get(ctx, p1.ID)
	if err != nil || got.ExportID == nil || got.ExportFileName == nil || *got.ExportFileName != "7701234567_40703810000000009981_1.txt" {
		t.Fatalf("p1 after export: %+v, err = %v", got, err)
	}
	pending, err := in.List(ctx, model.IncomingPaymentFilter{BankAccountID: &bank.ID, Export: ptr("pending")}, 50, 0)
	if err != nil || len(pending) != 2 { // p2 и платёж без ЛС
		t.Errorf("pending = %d, err = %v", len(pending), err)
	}
	done, err := in.List(ctx, model.IncomingPaymentFilter{BankAccountID: &bank.ID, Export: ptr("done")}, 50, 0)
	if err != nil || len(done) != 1 || done[0].ID != p1.ID {
		t.Errorf("done = %+v, err = %v", done, err)
	}

	exp2, err := exports.Create(ctx, onBank, ExportSelection{Mode: "filter", ExpectedCount: ptr(1)})
	if err != nil || exp2.Number != 2 || exp2.PaymentsCount != 1 || exp2.TotalAmount != 200 {
		t.Fatalf("second export = %+v, err = %v", exp2, err)
	}
	name, data, err := exports.File(ctx, exp2.ID)
	if err != nil {
		t.Fatal(err)
	}
	reg, errs, err := sberregistry.Parse(name, data)
	if err != nil || len(errs) != 0 || len(reg.Payments) != 1 || reg.Payments[0].AccountNum != "0009991001" || reg.Payments[0].PayerName != "ПЕТРОВ" {
		t.Fatalf("exported file: %+v, errs = %v, err = %v", reg, errs, err)
	}

	// Отзыв первой выгрузки возвращает платёж в «не выгружены»; номера не переиспользуются.
	if err := exports.Revoke(ctx, *got.ExportID); err != nil {
		t.Fatal(err)
	}
	if err := exports.Revoke(ctx, *got.ExportID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second revoke: err = %v, want not found", err)
	}
	if again, _ := in.Get(ctx, p1.ID); again.ExportID != nil {
		t.Errorf("p1 still exported: %+v", again)
	}
	exp3, err := exports.Create(ctx, onBank, ExportSelection{Mode: "filter"})
	if err != nil || exp3.Number != 3 {
		t.Fatalf("third export = %+v, err = %v", exp3, err)
	}
	if list, err := exports.List(ctx, model.PaymentExportFilter{BankAccountID: &bank.ID}, 50, 0); err != nil || len(list) != 2 {
		t.Errorf("active exports = %d, err = %v", len(list), err)
	}
}
