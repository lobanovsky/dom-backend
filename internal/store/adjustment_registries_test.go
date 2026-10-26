package store

import (
	"context"
	"errors"
	"testing"

	"dom-backend/internal/model"
	"dom-backend/internal/sberregistry"
)

func TestAdjustmentRegistries(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "adjust-test", INN: ptr("7701234567")})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "г Москва, ул Тестовая, 2"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	pr, err := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "10"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises", pr.ID)
	acc, err := NewAccounts(pool).Create(ctx, model.Account{Number: "0000010010", PremisesID: pr.ID, Purpose: "utilities", Status: "active", OpenedAt: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "personal_accounts", acc.ID)
	bank, err := NewBankAccounts(pool).Create(ctx, model.BankAccount{OrganizationID: org.ID, Number: "40703810000000009991", ValidFrom: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "bank_accounts", bank.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM adjustment_registries WHERE bank_account_id = $1`, bank.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM payment_exports WHERE bank_account_id = $1`, bank.ID)
	})

	s := NewAdjustmentRegistries(pool)
	req := model.AdjustmentRegistryRequest{BankAccountID: bank.ID, PersonalAccountID: acc.ID, Amount: 1234.56, PayerName: "Корректировка"}
	r1, err := s.Create(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Number != 1 || r1.Amount != 1234.56 || r1.AccountNumber != "0000010010" || r1.PremisesNumber != "10" || r1.ResolvedAt != nil {
		t.Fatalf("created = %+v", r1)
	}

	// Файл разбирается тем же разбором, что и настоящие реестры; номер операции из отдельного диапазона.
	name, data, err := s.File(ctx, r1.ID)
	if err != nil || name != "7701234567_40703810000000009991_1.txt" {
		t.Fatalf("file name = %q, err = %v", name, err)
	}
	got, errs, err := sberregistry.Parse(name, data)
	if err != nil || len(errs) != 0 || len(got.Payments) != 1 {
		t.Fatalf("parse: %+v %v %v", got, errs, err)
	}
	p := got.Payments[0]
	if p.AccountNum != "0000010010" || p.Amount != 123456 || p.PayerName != "Корректировка" || len(p.ExternalID) != 12 || p.ExternalID < "900000000000" {
		t.Errorf("payment = %+v", p)
	}

	// Номера общие с обычными выгрузками: следующий реестр получает следующий номер.
	if _, err := pool.Exec(ctx, `INSERT INTO payment_exports (bank_account_id, number, file_name, file_content, payments_count, total_amount) VALUES ($1, 2, 'x.txt', ''::bytea, 1, 1)`, bank.ID); err != nil {
		t.Fatal(err)
	}
	r2, err := s.Create(ctx, req)
	if err != nil || r2.Number != 3 {
		t.Fatalf("second = %+v, err = %v", r2, err)
	}

	// Отметка «снято», фильтры, удаление и восстановление.
	if r, err := s.SetResolved(ctx, r1.ID, true); err != nil || r.ResolvedAt == nil {
		t.Fatalf("resolve: %+v %v", r, err)
	}
	yes, no := true, false
	if items, _ := s.List(ctx, model.AdjustmentRegistryFilter{BankAccountID: &bank.ID, Resolved: &no}, 10, 0); len(items) != 1 || items[0].ID != r2.ID {
		t.Errorf("unresolved = %+v", items)
	}
	if items, _ := s.List(ctx, model.AdjustmentRegistryFilter{BankAccountID: &bank.ID, Resolved: &yes}, 10, 0); len(items) != 1 || items[0].ID != r1.ID {
		t.Errorf("resolved = %+v", items)
	}
	if r, err := s.SetResolved(ctx, r1.ID, false); err != nil || r.ResolvedAt != nil {
		t.Fatalf("unresolve: %+v %v", r, err)
	}
	if err := s.Delete(ctx, r2.ID); err != nil {
		t.Fatal(err)
	}
	if items, _ := s.List(ctx, model.AdjustmentRegistryFilter{BankAccountID: &bank.ID}, 10, 0); len(items) != 1 {
		t.Errorf("after delete = %+v", items)
	}
	if items, _ := s.List(ctx, model.AdjustmentRegistryFilter{BankAccountID: &bank.ID, Deleted: true}, 10, 0); len(items) != 1 || items[0].ID != r2.ID {
		t.Errorf("deleted = %+v", items)
	}
	if _, err := s.Restore(ctx, r2.ID); err != nil {
		t.Fatal(err)
	}

	// Ссылки на несуществующее — ошибка запроса; у организации без ИНН реестр не формируется.
	bad := req
	bad.PersonalAccountID = 1 << 40
	if _, err := s.Create(ctx, bad); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown account: err = %v", err)
	}
	bad = req
	bad.BankAccountID = 1 << 40
	if _, err := s.Create(ctx, bad); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown bank account: err = %v", err)
	}
	if err := s.Delete(ctx, 1<<40); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete unknown: err = %v", err)
	}
}
