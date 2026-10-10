package sbersync

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"dom-backend/internal/clientbank"
	"dom-backend/internal/sberapi"
	"dom-backend/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestZZProbe(t *testing.T) {
	if os.Getenv("PROBE") == "" {
		t.Skip()
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	bs := store.NewBankStatements(pool)
	data, _ := os.ReadFile("/Users/evgeny/Projects/tsn/dom/dom-backend/private/1c/kl_to_1c-2025-2026-10-08.txt")
	sts, rowErrs, err := clientbank.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("parsed", len(sts), "statements, row errors", len(rowErrs))
	_ = sts
	var c1, c2 int
	pool.QueryRow(ctx, "select count(*) from incoming_payments").Scan(&c1)
	pool.QueryRow(ctx, "select count(*) from outgoing_payments").Scan(&c2)
	t.Log("после 1С: входящих", c1, "исходящих", c2)

	st := store.NewSber(pool)
	hc, _, err := sberapi.NewHTTPClient(sberapi.TLSFiles{P12: "/Users/evgeny/Projects/tsn/dom/dom-backend/private/sber/prod/tls.p12", PasswordFile: "/Users/evgeny/Projects/tsn/dom/dom-backend/private/sber/prod/tls.pass", CADir: "/Users/evgeny/Projects/tsn/dom/dom-backend/private/sber/prod"})
	if err != nil {
		t.Fatal(err)
	}
	sec, _ := os.ReadFile("/Users/evgeny/Projects/tsn/dom/dom-backend/private/sber/prod/client_secret")
	cl := sberapi.New(sberapi.Config{ClientID: "92649", ClientSecret: strings.TrimSpace(string(sec))}, hc, st, slog.Default())
	sy := &Syncer{Source: cl, Statements: bs, Journal: st, Tokens: st, Log: slog.Default()}
	day := func(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
	res, err := sy.Sync(ctx, "manual", day("2026-08-01"), day("2026-10-08"))
	t.Logf("API 01..08.10: %+v err=%v", res, err)
	pool.QueryRow(ctx, "select count(*) from incoming_payments").Scan(&c1)
	pool.QueryRow(ctx, "select count(*) from outgoing_payments").Scan(&c2)
	t.Log("после API: входящих", c1, "исходящих", c2)
}
