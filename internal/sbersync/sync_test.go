package sbersync

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"dom-backend/internal/db"
	"dom-backend/internal/model"
	"dom-backend/internal/sberapi"
	"dom-backend/internal/store"
)

type fakeSource struct{ calls int }

const acc = "40703810999999999901"

func (f *fakeSource) Transactions(_ context.Context, account string, d time.Time) ([]sberapi.Transaction, []byte, error) {
	f.calls++
	// в БД могут быть и другие счета Сбера (например, настоящие в локальной базе): им банк ничего не отдаёт
	if account != acc || !d.Equal(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)) {
		return nil, []byte("{}\n"), nil
	}
	mk := func(direction, number, amount string) sberapi.Transaction {
		var t sberapi.Transaction
		b, _ := json.Marshal(map[string]any{
			"direction": direction, "number": number, "operationDate": "2026-10-09T10:00:00", "paymentPurpose": "sbersync-test",
			"amount": map[string]any{"amount": json.Number(amount)},
			"rurTransfer": map[string]any{"payerAccount": map[string]string{"CREDIT": "40817810000000000001", "DEBIT": account}[direction],
				"payeeAccount": map[string]string{"CREDIT": account, "DEBIT": "40702810000000000002"}[direction], "payerName": "sbersync-test", "payeeName": "sbersync-test"},
		})
		_ = json.Unmarshal(b, &t)
		return t
	}
	return []sberapi.Transaction{mk("CREDIT", "1", "100.10"), mk("DEBIT", "2", "50")}, []byte(`{"account":"` + account + `","day":"2026-10-09"}` + "\n"), nil
}

func TestSyncIsIdempotent(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	if err := db.Migrate(url); err != nil {
		t.Fatal(err)
	}
	pool, err := db.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	org, err := store.NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "sbersync-test"})
	if err != nil {
		t.Fatal(err)
	}
	bik := "044525225"
	ba, err := store.NewBankAccounts(pool).Create(ctx, model.BankAccount{OrganizationID: org.ID, Number: acc, BIK: &bik, ValidFrom: model.NewDate(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM incoming_payments WHERE bank_account_id = $1`, `DELETE FROM outgoing_payments WHERE bank_account_id = $1`,
			`DELETE FROM bank_statements WHERE bank_account_id = $1`, `DELETE FROM bank_accounts WHERE id = $1`,
		} {
			if _, err := pool.Exec(ctx, q, ba.ID); err != nil {
				t.Error(err)
			}
		}
		pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, org.ID)
	})

	sber := store.NewSber(pool)
	// локальная БД может хранить настоящие токены: после теста возвращаем как было
	prev, prevErr := sber.Tokens(ctx)
	pool.Exec(ctx, `DELETE FROM sber_tokens`) // тест начинается без токенов
	var maxRun int64
	pool.QueryRow(ctx, `SELECT COALESCE(MAX(id), 0) FROM sber_sync_runs`).Scan(&maxRun)
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM sber_sync_runs WHERE id > $1`, maxRun)
		if prevErr == nil {
			sber.SaveTokens(ctx, prev)
		} else {
			pool.Exec(ctx, `DELETE FROM sber_tokens`)
		}
	})
	s := &Syncer{Source: &fakeSource{}, Statements: store.NewBankStatements(pool), Journal: sber, Tokens: sber, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	from, to := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)

	if _, err := s.Sync(ctx, "manual", from, to); err != ErrNotConfigured {
		t.Fatalf("without tokens: err = %v, want ErrNotConfigured", err)
	}
	if err := sber.SaveTokens(ctx, model.SberTokens{AccessToken: "a", RefreshToken: "r", AccessExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	res, err := s.Sync(ctx, "manual", from, to)
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("first sync: %+v, %v", res, err)
	}
	// в базе могут быть и другие счета Сбера; наш вклад проверяем по платежам счёта
	var in, out int
	pool.QueryRow(ctx, `SELECT count(*) FROM incoming_payments WHERE bank_account_id = $1`, ba.ID).Scan(&in)
	pool.QueryRow(ctx, `SELECT count(*) FROM outgoing_payments WHERE bank_account_id = $1`, ba.ID).Scan(&out)
	if in != 1 || out != 1 {
		t.Fatalf("payments = %d in, %d out; want 1 and 1", in, out)
	}

	// повтор: тот же ответ банка — ничего нового; ответ изменился (другой файл), но операции те же — дубли отсекает ключ
	res, err = s.Sync(ctx, "manual", from, to)
	if err != nil || len(res.Errors) != 0 || res.Incoming != 0 || res.Outgoing != 0 {
		t.Fatalf("second sync: %+v, %v", res, err)
	}
	res, err = s.Sync(ctx, "manual", from.AddDate(0, 0, -1), to)
	if err != nil || res.Incoming != 0 || res.Outgoing != 0 || res.Skipped < 2 {
		t.Fatalf("wider period: %+v, %v", res, err)
	}
	pool.QueryRow(ctx, `SELECT count(*) FROM incoming_payments WHERE bank_account_id = $1`, ba.ID).Scan(&in)
	if in != 1 {
		t.Errorf("incoming after repeats = %d, want 1", in)
	}

	// параллельный запуск отклоняется
	unlock, ok, err := sber.TryLock(ctx)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if _, err := s.Sync(ctx, "manual", from, to); err != ErrBusy {
		t.Errorf("concurrent: err = %v, want ErrBusy", err)
	}
	unlock()
}

func TestNextRun(t *testing.T) {
	msk, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	at := func(y int, m time.Month, d, h, min int, loc *time.Location) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, loc)
	}
	for name, tc := range map[string]struct{ now, want time.Time }{
		"до времени запуска — сегодня":     {at(2026, 10, 10, 0, 30, msk), at(2026, 10, 10, 1, 0, msk)},
		"после времени запуска — завтра":   {at(2026, 10, 10, 1, 30, msk), at(2026, 10, 11, 1, 0, msk)},
		"ровно в это время — завтра":       {at(2026, 10, 10, 1, 0, msk), at(2026, 10, 11, 1, 0, msk)},
		"конец месяца":                     {at(2026, 10, 31, 12, 0, msk), at(2026, 11, 1, 1, 0, msk)},
		"сервер в UTC: в Москве уже 01:30": {at(2026, 10, 10, 22, 30, time.UTC), at(2026, 10, 12, 1, 0, msk)}, // 22:30 UTC = 01:30 МСК 11 октября
	} {
		got, err := NextRun(tc.now, "01:00", msk)
		if err != nil || !got.Equal(tc.want) {
			t.Errorf("%s: got %v, want %v (err %v)", name, got, tc.want, err)
		}
	}
	if _, err := NextRun(time.Now(), "25:00", msk); err == nil {
		t.Error("invalid time must be rejected")
	}
}
