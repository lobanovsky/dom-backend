package sberapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dom-backend/internal/clientbank"
	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

type memTokens struct {
	mu sync.Mutex
	t  *model.SberTokens
}

func (m *memTokens) Tokens(context.Context) (model.SberTokens, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.t == nil {
		return model.SberTokens{}, &store.Error{Kind: store.ErrNotFound, Msg: "no tokens"}
	}
	return *m.t, nil
}

func (m *memTokens) SaveTokens(_ context.Context, t model.SberTokens) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.t = &t
	return nil
}

func testClient(t *testing.T, h http.Handler, tokens *memTokens) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(Config{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "secret"}, srv.Client(), tokens, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

const acc = "40703810338000004376"

func tx(direction, number, amount, payer, payee, purpose string) map[string]any {
	return map[string]any{
		"direction": direction, "number": number, "operationCode": "01", "operationDate": "2026-10-09T12:30:00",
		"paymentPurpose": purpose, "amount": map[string]any{"amount": json.Number(amount)},
		"rurTransfer": map[string]any{"payerAccount": payer, "payeeAccount": payee, "payerName": "Плательщик", "payeeName": "Получатель", "payerInn": "7701", "payeeInn": "7702"},
	}
}

func TestTransactionsPagesAndRefresh(t *testing.T) {
	var tokenCalls, stmtCalls int
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ic/sso/api/v2/oauth/token":
			tokenCalls++
			r.ParseForm()
			if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("refresh_token") != "R1" || r.PostForm.Get("client_secret") != "secret" {
				t.Errorf("token form = %v", r.PostForm)
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "A2", "refresh_token": "R2", "expires_in": 3600})
		case "/fintech/api/v2/statement/transactions":
			stmtCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer A2" {
				t.Errorf("Authorization = %q", got)
			}
			q := r.URL.Query()
			if q.Get("accountNumber") != acc || q.Get("statementDate") != "2026-10-09" {
				t.Errorf("query = %v", q)
			}
			if q.Get("page") == "1" {
				json.NewEncoder(w).Encode(map[string]any{"transactions": []any{tx("CREDIT", "1", "10.50", "40817810000000000001", acc, "ЛС 1")}, "_links": []any{map[string]any{"rel": "next", "href": "?page=2"}}})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"transactions": []any{tx("DEBIT", "2", "3", acc, "40702810000000000002", "Оплата")}})
		default:
			http.NotFound(w, r)
		}
	})
	// пустой access_token и нулевой срок — как после ввода refresh_token на странице
	tokens := &memTokens{t: &model.SberTokens{RefreshToken: "R1"}}
	c := testClient(t, h, tokens)
	txs, raw, err := c.Transactions(context.Background(), acc, time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 2 || stmtCalls != 2 || tokenCalls != 1 {
		t.Fatalf("txs=%d stmtCalls=%d tokenCalls=%d", len(txs), stmtCalls, tokenCalls)
	}
	if got, _ := tokens.Tokens(context.Background()); got.RefreshToken != "R2" || got.AccessToken != "A2" {
		t.Errorf("new tokens not saved: %+v", got)
	}
	if !strings.Contains(string(raw), `"number":"2"`) {
		t.Errorf("raw does not hold both pages: %s", raw)
	}
}

func TestUnauthorizedRefreshesOnce(t *testing.T) {
	var tokenCalls int
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ic/sso/api/v2/oauth/token" {
			tokenCalls++
			json.NewEncoder(w).Encode(map[string]any{"access_token": fmt.Sprintf("B%d", tokenCalls), "refresh_token": "R", "expires_in": 3600})
			return
		}
		if r.Header.Get("Authorization") == "Bearer A1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"transactions": []any{}})
	})
	// токен ещё "живой" по сроку, но банк его отвергает
	tokens := &memTokens{t: &model.SberTokens{AccessToken: "A1", RefreshToken: "R", AccessExpiresAt: time.Now().Add(time.Hour)}}
	c := testClient(t, h, tokens)
	if _, _, err := c.Transactions(context.Background(), acc, time.Now()); err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 1 {
		t.Errorf("tokenCalls = %d, want 1", tokenCalls)
	}
}

func TestAcceptedIsRetriedAndErrorsAreShort(t *testing.T) {
	var calls int
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch {
		case calls <= 2:
			w.WriteHeader(http.StatusAccepted)
		case calls == 3:
			json.NewEncoder(w).Encode(map[string]any{"transactions": []any{}})
		}
	})
	tokens := &memTokens{t: &model.SberTokens{AccessToken: "A", RefreshToken: "R", AccessExpiresAt: time.Now().Add(time.Hour)}}
	c := testClient(t, h, tokens)
	if _, _, err := c.Transactions(context.Background(), acc, time.Now()); err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}

	h2 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"cause":"FORBIDDEN","message":"no access","secret":"x"}`))
	})
	c = testClient(t, h2, tokens)
	_, _, err := c.Transactions(context.Background(), acc, time.Now())
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != 403 || !strings.Contains(apiErr.Message, "no access") || strings.Contains(apiErr.Message, "secret") {
		t.Fatalf("err = %v", err)
	}
}

func TestStatementMappingAndDedupKeyMatches1C(t *testing.T) {
	day := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	var txs []Transaction
	for _, m := range []map[string]any{
		tx("CREDIT", "7", "1234.56", "40817810000000000001", acc, "  ЛС 0000001101  "),
		tx("CREDIT", "7", "1234.56", "40817810000000000001", acc, "ЛС 0000001101"), // два одинаковых платежа
		tx("DEBIT", "8", "100", acc, "40702810000000000002", "Оплата услуг"),
	} {
		b, _ := json.Marshal(m)
		var t Transaction
		if err := json.Unmarshal(b, &t); err != nil {
			panic(err)
		}
		txs = append(txs, t)
	}
	st, err := Statement(acc, day, day, txs)
	if err != nil {
		t.Fatal(err)
	}
	if st.CreditCount != 2 || st.DebitCount != 1 || st.CreditTotal != 246912 || st.DebitTotal != 10000 {
		t.Fatalf("totals: %+v", st)
	}
	in, out := st.Operations[0], st.Operations[2]
	if in.Outgoing || in.CounterName != "Плательщик" || in.CounterAccount != "40817810000000000001" || in.Purpose != "ЛС 0000001101" || in.Amount != 123456 {
		t.Errorf("incoming = %+v", in)
	}
	if !out.Outgoing || out.CounterName != "Получатель" || out.CounterAccount != "40702810000000000002" {
		t.Errorf("outgoing = %+v", out)
	}
	if in.DedupKey == st.Operations[1].DedupKey {
		t.Error("identical payments must get different keys")
	}
	// ключ тот же, что у операции из файла 1С с теми же значениями полей
	same := model.StatementOperation{At: day, Amount: 123456, CounterAccount: "40817810000000000001", DocNumber: "7", Purpose: "ЛС 0000001101"}
	if want := clientbank.DedupKey(same, map[string]int{}); in.DedupKey != want {
		t.Errorf("dedup key differs from the 1C one")
	}

	// счёт не на своём месте — ошибка, а не молчаливая запись в другую сторону
	if _, err := Statement("40703810000000000099", day, day, txs[:1]); err == nil {
		t.Error("expected an error for a foreign account")
	}
}

func TestKopecks(t *testing.T) {
	for in, want := range map[string]int64{"1.01": 101, "0": 0, "1234.5": 123450, "1E+3": 100000, "0.29": 29} {
		if got, err := kopecks(json.Number(in)); err != nil || got != want {
			t.Errorf("kopecks(%s) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := kopecks("1.005"); err == nil {
		t.Error("fractions of a kopeck must be rejected")
	}
}

func TestRejectedRefreshTokenIsAuthError(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"expired"}`))
	})
	tokens := &memTokens{t: &model.SberTokens{RefreshToken: "old"}}
	c := testClient(t, h, tokens)
	_, _, err := c.Transactions(context.Background(), acc, time.Now())
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Status != 400 || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := tokens.Tokens(context.Background()); got.RefreshToken != "old" {
		t.Error("a rejected refresh must not overwrite the saved token")
	}
}

func TestOneLine(t *testing.T) {
	if got, want := oneLine("ЕНП за 2026 года\r\nБез  НДС. "), "ЕНП за 2026 годаБез НДС."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
