package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"dom-backend/internal/model"
)

func TestBankAccountsAndPayments(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "pay-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)

	banks, cats := NewBankAccounts(pool), NewPaymentCategories(pool)
	in, out := NewIncomingPayments(pool), NewOutgoingPayments(pool)

	bank, err := banks.Create(ctx, model.BankAccount{
		OrganizationID: org.ID, Number: "40703810000000009991", ValidFrom: date(2020, 1, 1), IsSpecial: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "bank_accounts", bank.ID)
	if !bank.Active {
		t.Errorf("account with open period must be active: %+v", bank)
	}
	closed, err := banks.Create(ctx, model.BankAccount{
		OrganizationID: org.ID, Number: "40703810000000009992", ValidFrom: date(2019, 1, 1), ValidTo: ptr(date(2019, 12, 31)),
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "bank_accounts", closed.ID)
	if closed.Active {
		t.Errorf("account with past valid_to must not be active: %+v", closed)
	}
	if list, err := banks.List(ctx, model.BankAccountFilter{OrganizationID: &org.ID, Active: ptr(true)}, 50, 0); err != nil || len(list) != 1 || list[0].ID != bank.ID {
		t.Errorf("active filter: %+v, err = %v", list, err)
	}
	if _, err := banks.Create(ctx, model.BankAccount{OrganizationID: org.ID, Number: bank.Number, ValidFrom: date(2020, 1, 1)}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate account number: err = %v, want conflict", err)
	}

	cat, err := cats.Create(ctx, model.PaymentCategory{Name: "pay-test аренда", Direction: "incoming"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "payment_categories", cat.ID)

	clock := model.Clock("09:32:33")
	p1, err := in.Create(ctx, model.IncomingPayment{
		BankAccountID: bank.ID, ExternalID: ptr("pay-test-1"), PaymentDate: date(2026, 1, 3), PaymentTime: &clock,
		Amount: 8575.32, Commission: ptr(0.0), PayerName: "ИВАНОВ ИВАН", Purpose: ptr("ЖКУ за январь"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "incoming_payments", p1.ID)
	if p1.Amount != 8575.32 || p1.PaymentTime == nil || *p1.PaymentTime != clock || p1.PaymentDate.Format("2006-01-02") != "2026-01-03" {
		t.Errorf("incoming round trip: %+v", p1)
	}
	p2, err := in.Create(ctx, model.IncomingPayment{
		BankAccountID: bank.ID, PaymentDate: date(2026, 2, 10), Amount: 1000, PayerName: "ООО Ромашка", CategoryID: &cat.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "incoming_payments", p2.ID)

	ids := func(f model.IncomingPaymentFilter) []int64 {
		f.BankAccountID = &bank.ID
		items, err := in.List(ctx, f, 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		var got []int64
		for _, p := range items {
			got = append(got, p.ID)
		}
		return got
	}
	for name, c := range map[string]struct {
		f    model.IncomingPaymentFilter
		want []int64
	}{
		"all, newest first": {model.IncomingPaymentFilter{}, []int64{p2.ID, p1.ID}},
		"date range":        {model.IncomingPaymentFilter{DateTo: ptr(date(2026, 1, 31))}, []int64{p1.ID}},
		"amount from":       {model.IncomingPaymentFilter{AmountFrom: ptr(5000.0)}, []int64{p1.ID}},
		"q by payer":        {model.IncomingPaymentFilter{Q: ptr("ромашка")}, []int64{p2.ID}},
		"q by purpose":      {model.IncomingPaymentFilter{Q: ptr("жку за")}, []int64{p1.ID}},
		"unlinked":          {model.IncomingPaymentFilter{Unlinked: true}, []int64{p1.ID}},
		"category":          {model.IncomingPaymentFilter{CategoryID: &cat.ID}, []int64{p2.ID}},
	} {
		got := ids(c.f)
		if len(got) != len(c.want) || (len(got) > 0 && got[0] != c.want[0]) {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}

	upd := p2
	upd.Amount = 1200.5
	if got, err := in.Update(ctx, p2.ID, upd); err != nil || got.Amount != 1200.5 || got.CategoryID == nil {
		t.Errorf("update: %+v, err = %v", got, err)
	}

	// поиск q находит платёж и по номеру лицевого счёта (в TestRegistryImport проверяется привязка)

	// повтор номера операции на том же счёте — конфликт; CHECK «либо лицевой счёт, либо категория»
	if _, err := in.Create(ctx, model.IncomingPayment{BankAccountID: bank.ID, ExternalID: ptr("pay-test-1"), PaymentDate: date(2026, 1, 4), Amount: 1, PayerName: "X"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate external_id: err = %v, want conflict", err)
	}

	// банковский счёт с действующими платежами удалить нельзя
	var se *Error
	if err := banks.Delete(ctx, bank.ID); !errors.As(err, &se) || !errors.Is(se.Kind, ErrInvalid) {
		t.Errorf("delete account with payments: err = %v, want invalid", err)
	}
	// удалённый платёж не показывается, но его номер операции остаётся занятым
	if err := in.Delete(ctx, p1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Create(ctx, model.IncomingPayment{BankAccountID: bank.ID, ExternalID: ptr("pay-test-1"), PaymentDate: date(2026, 1, 4), Amount: 1, PayerName: "X"}); !errors.Is(err, ErrConflict) {
		t.Errorf("external_id of a deleted payment must stay reserved: err = %v", err)
	}
	if _, err := in.Restore(ctx, p1.ID); err != nil {
		t.Errorf("restore: %v", err)
	}

	o1, err := out.Create(ctx, model.OutgoingPayment{
		BankAccountID: bank.ID, PaymentDate: date(2026, 1, 20), Amount: 15000.5, RecipientName: "ООО Лифт", CategoryID: nil, Purpose: ptr("обслуживание лифтов"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "outgoing_payments", o1.ID)
	if items, err := out.List(ctx, model.OutgoingPaymentFilter{BankAccountID: &bank.ID, Q: ptr("лифт")}, 50, 0); err != nil || len(items) != 1 || items[0].Amount != 15000.5 {
		t.Errorf("outgoing list: %+v, err = %v", items, err)
	}
	// категорию с действующими платежами удалить нельзя
	if err := cats.Delete(ctx, cat.ID); !errors.As(err, &se) || !errors.Is(se.Kind, ErrInvalid) {
		t.Errorf("delete category with payments: err = %v, want invalid", err)
	}
}

func TestRegistryImport(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "reg-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	bank, err := NewBankAccounts(pool).Create(ctx, model.BankAccount{OrganizationID: org.ID, Number: "40703810000000009993", ValidFrom: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "bank_accounts", bank.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "reg-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	pr, err := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "1"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises", pr.ID)
	acc, err := NewAccounts(pool).Create(ctx, model.Account{Number: "reg-test-1", PremisesID: pr.ID, Purpose: "utilities", Status: "active", OpenedAt: date(2026, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "personal_accounts", acc.ID)
	accKR, err := NewAccounts(pool).Create(ctx, model.Account{Number: "reg-test-kr", PremisesID: pr.ID, Purpose: "capital_repair", Status: "active", OpenedAt: date(2026, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "personal_accounts", accKR.ID)
	// платежи и реестры удаляются раньше счетов (cleanup выполняются в обратном порядке)
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM incoming_payments WHERE bank_account_id = $1`, `DELETE FROM payment_registries WHERE bank_account_id = $1`} {
			if _, err := pool.Exec(ctx, q, bank.ID); err != nil {
				t.Error(err)
			}
		}
	})

	pay := func(id, account string, kop int64) model.RegistryPayment {
		return model.RegistryPayment{Line: 1, Date: date(2026, 1, 3), Time: "09:32:33", ExternalID: id, AccountNum: account,
			PayerName: "ТЕСТОВ ТЕСТ", Amount: kop, Raw: "raw " + id}
	}
	regs := NewPaymentRegistries(pool)
	reg := &model.ParsedRegistry{FileAccount: bank.Number, RegistryNumber: "42", RegistryDate: ptr(date(2026, 1, 6)),
		Payments: []model.RegistryPayment{pay("reg-test-a", "reg-test-1", 857532), pay("reg-test-b", "no-such-account", 100)}, TotalAmount: 857632}

	res, err := regs.Import(ctx, bank.ID, "reg-1.txt", []byte("file one"), reg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 2 || res.Linked != 1 || res.Unlinked != 1 || res.SkippedDuplicates != 0 || res.RegistryID == 0 {
		t.Fatalf("first import: %+v", res)
	}
	got, err := NewIncomingPayments(pool).List(ctx, model.IncomingPaymentFilter{RegistryID: &res.RegistryID}, 50, 0)
	if err != nil || len(got) != 2 {
		t.Fatalf("registry payments: %+v, err = %v", got, err)
	}
	for _, p := range got {
		if p.ExternalID == nil || (*p.ExternalID == "reg-test-a") != (p.PersonalAccountID != nil && *p.PersonalAccountID == acc.ID) || p.RawLine == nil {
			t.Errorf("payment link/raw: %+v", p)
		}
	}
	if got, err := NewIncomingPayments(pool).List(ctx, model.IncomingPaymentFilter{BankAccountID: &bank.ID, Q: ptr("reg-test-1")}, 50, 0); err != nil || len(got) != 1 {
		t.Errorf("search by personal account number: %+v, err = %v", got, err)
	}
	if r, err := regs.Get(ctx, res.RegistryID); err != nil || r.PaymentsCount != 2 || r.TotalAmount != 8576.32 {
		t.Errorf("registry row: %+v, err = %v", r, err)
	}
	if name, data, err := regs.File(ctx, res.RegistryID); err != nil || name != "reg-1.txt" || string(data) != "file one" {
		t.Errorf("stored file: %q %q, err = %v", name, data, err)
	}
	if list, err := regs.List(ctx, model.PaymentRegistryFilter{BankAccountID: &bank.ID, Q: ptr("reg-1")}, 50, 0); err != nil || len(list) != 1 {
		t.Errorf("registry search: %+v, err = %v", list, err)
	}

	var se *Error
	// тот же файл: отказ целиком
	var exists *RegistryExistsError
	if _, err := regs.Import(ctx, bank.ID, "reg-1.txt", []byte("file one"), reg); !errors.As(err, &exists) || exists.RegistryID != res.RegistryID || exists.FileName != "reg-1.txt" || !errors.Is(err, ErrConflict) {
		t.Errorf("same file: err = %v, want RegistryExistsError(%d)", err, res.RegistryID)
	}
	// другой файл с пересечением: старый платёж пропускается, новый загружается
	overlap := &model.ParsedRegistry{FileAccount: bank.Number, Payments: []model.RegistryPayment{pay("reg-test-a", "reg-test-1", 857532), pay("reg-test-c", "reg-test-1", 500)}}
	res2, err := regs.Import(ctx, bank.ID, "reg-2.txt", []byte("file two"), overlap)
	if err != nil || res2.Created != 1 || res2.SkippedDuplicates != 1 || res2.Skipped[0].ExternalID != "reg-test-a" {
		t.Fatalf("overlapping import: %+v, err = %v", res2, err)
	}
	// капремонт на обычный банковский счёт: платёж привязан как есть, в комментарии пометка, в отчёте предупреждение;
	// платёж без ФИО допустим
	mismatch := &model.ParsedRegistry{FileAccount: bank.Number, Payments: []model.RegistryPayment{pay("reg-test-e", "reg-test-kr", 7245960)}}
	mismatch.Payments[0].PayerName = ""
	res3, err := regs.Import(ctx, bank.ID, "reg-mismatch.txt", []byte("file mismatch"), mismatch)
	if err != nil || res3.Created != 1 || res3.Linked != 1 || len(res3.Warnings) != 1 {
		t.Fatalf("type mismatch import: %+v, err = %v", res3, err)
	}
	if got, err := NewIncomingPayments(pool).List(ctx, model.IncomingPaymentFilter{RegistryID: &res3.RegistryID}, 50, 0); err != nil || len(got) != 1 ||
		got[0].PersonalAccountID == nil || *got[0].PersonalAccountID != accKR.ID || got[0].Comment == nil || got[0].PayerName != "" {
		t.Errorf("mismatch payment: %+v, err = %v", got, err)
	}
	// все платежи уже известны: реестр не создаётся
	all := &model.ParsedRegistry{FileAccount: bank.Number, Payments: []model.RegistryPayment{pay("reg-test-a", "reg-test-1", 857532)}}
	var allDup *AllDuplicatesError
	if _, err := regs.Import(ctx, bank.ID, "reg-3.txt", []byte("file three"), all); !errors.As(err, &allDup) || allDup.Total != 1 || len(allDup.Skipped) != 1 || !errors.Is(err, ErrConflict) {
		t.Errorf("all known: err = %v, want AllDuplicatesError", err)
	}
	if byNumber, err := regs.BankAccountsByNumber(ctx); err != nil || byNumber[bank.Number] != bank.ID {
		t.Errorf("BankAccountsByNumber: %v, err = %v", byNumber[bank.Number], err)
	}
	// счёт из имени файла не совпадает с выбранным
	wrong := &model.ParsedRegistry{FileAccount: "40703810000000000000", Payments: []model.RegistryPayment{pay("reg-test-d", "x", 100)}}
	if _, err := regs.Import(ctx, bank.ID, "reg-4.txt", []byte("file four"), wrong); !errors.As(err, &se) || !errors.Is(se.Kind, ErrInvalid) {
		t.Errorf("wrong account: err = %v, want invalid", err)
	}
	if _, err := regs.Import(ctx, 1<<40, "reg-5.txt", []byte("five"), reg); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing bank account: err = %v", err)
	}
	if list, err := NewIncomingPayments(pool).List(ctx, model.IncomingPaymentFilter{BankAccountID: &bank.ID}, 50, 0); err != nil || len(list) != 4 {
		t.Errorf("failed imports must not leave payments: %d, err = %v", len(list), err)
	}
}

func TestStatementImportAndOverlap(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "stmt-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	const acct = "40703810000000009977"
	bank, err := NewBankAccounts(pool).Create(ctx, model.BankAccount{OrganizationID: org.ID, Number: acct, ValidFrom: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "bank_accounts", bank.ID)
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM incoming_payments WHERE bank_account_id = $1`, `DELETE FROM outgoing_payments WHERE bank_account_id = $1`, `DELETE FROM bank_statements WHERE bank_account_id = $1`} {
			if _, err := pool.Exec(ctx, q, bank.ID); err != nil {
				t.Error(err)
			}
		}
	})

	at := func(d int) time.Time { return time.Date(2026, 1, d, 9, 30, 15, 0, time.UTC) }
	in := func(key string, d int, kop int64, name string) model.StatementOperation {
		return model.StatementOperation{Row: d, At: at(d), Amount: kop, CounterAccount: "40817810100000000001", CounterINN: "504908996115", CounterName: name,
			DocNumber: "d" + key, OperationType: "01", BIK: "044525974", BankName: "ТБанк", Purpose: "ЛС 0000001101 " + key, Raw: "raw " + key, DedupKey: key}
	}
	out := in("k3", 10, 5000, "ООО Лифт")
	out.Outgoing = true
	open1, close1 := int64(100000), int64(150000)
	stA := &model.ParsedStatement{Account: acct, PeriodFrom: ptr(date(2026, 1, 1)), PeriodTo: ptr(date(2026, 1, 10)), OpeningBalance: &open1, ClosingBalance: &close1,
		DebitCount: 1, CreditCount: 2, DebitTotal: 5000, CreditTotal: 30000,
		Operations: []model.StatementOperation{in("k1", 5, 10000, "ИВАНОВ ИВАН"), in("k2", 10, 20000, "ПЕТРОВ ПЕТР"), out}}

	stmts := NewBankStatements(pool)
	res, err := stmts.Import(ctx, "A.xlsx", []byte("file A"), stA)
	if err != nil || res.Incoming != 2 || res.Outgoing != 1 || res.SkippedDuplicates != 0 || res.StatementID == 0 {
		t.Fatalf("first import: %+v, err = %v", res, err)
	}
	got, err := NewIncomingPayments(pool).List(ctx, model.IncomingPaymentFilter{StatementID: &res.StatementID}, 50, 0)
	if err != nil || len(got) != 2 {
		t.Fatalf("incoming of the statement: %+v, err = %v", got, err)
	}
	var ivan model.IncomingPayment
	for _, p := range got {
		if p.PayerName == "ИВАНОВ ИВАН" {
			ivan = p
		}
	}
	if ivan.Amount != 100 || ivan.PaymentTime != nil || ivan.PayerINN == nil || *ivan.PayerINN != "504908996115" ||
		ivan.DocNumber == nil || *ivan.DocNumber != "dk1" || ivan.RawLine == nil || ivan.PersonalAccountID != nil || ivan.StatementID == nil {
		t.Errorf("imported incoming payment: %+v", ivan)
	}
	if o, err := NewOutgoingPayments(pool).List(ctx, model.OutgoingPaymentFilter{StatementID: &res.StatementID}, 50, 0); err != nil || len(o) != 1 || o[0].RecipientName != "ООО Лифт" || o[0].Amount != 50 {
		t.Errorf("outgoing of the statement: %+v, err = %v", o, err)
	}
	if s, err := stmts.Get(ctx, res.StatementID); err != nil || s.OpeningBalance == nil || *s.OpeningBalance != 1000 || *s.ClosingBalance != 1500 || s.CreditCount != 2 || s.PeriodTo.Format("2006-01-02") != "2026-01-10" {
		t.Errorf("statement row: %+v, err = %v", s, err)
	}

	// тот же файл — отказ; пересекающаяся выписка (k2 уже есть) — дубль пропускается, новое загружается
	var exists *StatementExistsError
	if _, err := stmts.Import(ctx, "A-copy.xlsx", []byte("file A"), stA); !errors.As(err, &exists) || exists.StatementID != res.StatementID || exists.FileName != "A.xlsx" {
		t.Errorf("same file: err = %v", err)
	}
	stB := &model.ParsedStatement{Account: acct, DebitCount: 0, CreditCount: 2,
		Operations: []model.StatementOperation{in("k2", 10, 20000, "ПЕТРОВ ПЕТР"), in("k4", 12, 7000, "СИДОРОВ СИДОР")}}
	resB, err := stmts.Import(ctx, "B.xlsx", []byte("file B"), stB)
	if err != nil || resB.Incoming != 1 || resB.SkippedDuplicates != 1 || resB.Skipped[0].Counterpart != "ПЕТРОВ ПЕТР" {
		t.Fatalf("overlapping import: %+v, err = %v", resB, err)
	}
	// удалённая операция остаётся «занятой»; всё известно — выписка не создаётся
	var allDup *StatementAllDuplicatesError
	stC := &model.ParsedStatement{Account: acct, Operations: []model.StatementOperation{in("k1", 5, 10000, "ИВАНОВ ИВАН"), out}}
	if _, err := stmts.Import(ctx, "C.xlsx", []byte("file C"), stC); !errors.As(err, &allDup) || allDup.Total != 2 || !errors.Is(err, ErrConflict) {
		t.Errorf("all known: err = %v", err)
	}
	// выписки по двум листам одного файла: ключ файла включает лист, поэтому вторая не считается дублем первой
	two := func(sheet string) *model.ParsedStatement {
		return &model.ParsedStatement{Account: acct, Part: sheet, MultiPart: true, DebitCount: 0, CreditCount: 1,
			Operations: []model.StatementOperation{in("k-"+sheet, 15, 3000, "СИДОРОВ")}}
	}
	if _, err := stmts.Import(ctx, "multi.xlsx — лист a", []byte("multi file"), two("a")); err != nil {
		t.Fatalf("sheet a: %v", err)
	}
	if _, err := stmts.Import(ctx, "multi.xlsx — лист b", []byte("multi file"), two("b")); err != nil {
		t.Fatalf("sheet b of the same file must be accepted: %v", err)
	}
	if _, err := stmts.Import(ctx, "multi.xlsx — лист a", []byte("multi file"), two("a")); !errors.As(err, &exists) {
		t.Errorf("the same sheet twice must be a duplicate: %v", err)
	}
	var unknown *UnknownBankAccountError
	if _, err := stmts.Import(ctx, "D.xlsx", []byte("file D"), &model.ParsedStatement{Account: "40703810000000000000", Operations: stC.Operations}); !errors.As(err, &unknown) || !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown account: err = %v", err)
	}
	if list, err := stmts.List(ctx, model.BankStatementFilter{BankAccountID: &bank.ID}, 50, 0); err != nil || len(list) != 4 {
		t.Errorf("failed imports must not leave statements: %d, err = %v", len(list), err)
	}
	if list, err := stmts.List(ctx, model.BankStatementFilter{BankAccountID: &bank.ID, Q: ptr("b.xl")}, 50, 0); err != nil || len(list) != 1 {
		t.Errorf("search by file name: %d, err = %v", len(list), err)
	}
}
