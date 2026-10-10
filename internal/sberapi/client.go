// Package sberapi — клиент Sber API для чтения банковских выписок (только чтение).
//
// Авторизация: mTLS (клиентский сертификат) + OAuth2 access_token. Токены выдаются в личном кабинете Sber API,
// дальше клиент сам обновляет их по refresh_token; refresh_token при каждом обновлении меняется, поэтому
// новая пара сохраняется в TokenStore сразу.
package sberapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"dom-backend/internal/model"
)

const DefaultBaseURL = "https://fintech.sberbank.ru:9443"

// TokenStore хранит пару токенов между запусками.
type TokenStore interface {
	Tokens(ctx context.Context) (model.SberTokens, error)
	SaveTokens(ctx context.Context, t model.SberTokens) error
}

type Config struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
}

type Client struct {
	cfg    Config
	http   *http.Client
	store  TokenStore
	mu     sync.Mutex // сериализует обновление токенов: refresh_token одноразовый
	now    func() time.Time
	sleep  func(ctx context.Context, d time.Duration) error
	log    *slog.Logger
	pause  time.Duration // пауза между повторами при 202 (выписка формируется)
	pauses int           // сколько раз ждём 202
}

func New(cfg Config, httpClient *http.Client, store TokenStore, log *slog.Logger) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	return &Client{
		cfg: cfg, http: httpClient, store: store, now: time.Now, log: log,
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
		pause: 15 * time.Second, pauses: 8,
	}
}

// APIError — ответ банка с ошибкой. Тело не логируем целиком: в нём могут быть персональные данные.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("sber api: status %d: %s", e.Status, e.Message) }

// AuthError — банк не принял токены (refresh_token недействителен или протух, неверный client_secret).
// Повторять с другим счётом бессмысленно: нужен новый токен из личного кабинета.
type AuthError struct{ APIError }

func (e *AuthError) Unwrap() error { return &e.APIError }

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// accessToken возвращает действующий токен; обновляет, если до конца жизни меньше 5 минут или force.
func (c *Client) accessToken(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, err := c.store.Tokens(ctx)
	if err != nil {
		return "", err
	}
	if !force && c.now().Add(5*time.Minute).Before(t.AccessExpiresAt) {
		return t.AccessToken, nil
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"refresh_token": {t.RefreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/ic/sso/api/v2/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	body, status, err := c.send(req)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", &AuthError{APIError{Status: status, Message: "token refresh failed: " + errorText(body)}}
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil || tr.AccessToken == "" || tr.RefreshToken == "" {
		return "", errors.New("sber api: unexpected token response")
	}
	if tr.ExpiresIn <= 0 {
		tr.ExpiresIn = 3600
	}
	nt := model.SberTokens{AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken, AccessExpiresAt: c.now().Add(time.Duration(tr.ExpiresIn) * time.Second)}
	if err := c.store.SaveTokens(ctx, nt); err != nil {
		// Банк уже выдал новую пару, а старый refresh_token какое-то время ещё действует (резерв 2 часа): ошибку наверх.
		return "", fmt.Errorf("sber api: new tokens were issued but could not be saved: %w", err)
	}
	c.log.Info("sber tokens refreshed")
	return nt.AccessToken, nil
}

func (c *Client) send(req *http.Request) ([]byte, int, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("sber api: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return body, resp.StatusCode, err
}

// errorText достаёт короткое описание ошибки банка (cause/message/error_description) без остального тела.
func errorText(body []byte) string {
	var e struct {
		Cause, Message, Error string
		Description           string `json:"error_description"`
	}
	if json.Unmarshal(body, &e) != nil {
		return "unreadable response"
	}
	var parts []string
	for _, p := range []string{e.Cause, e.Error, e.Message, e.Description} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return "no details"
	}
	return strings.Join(parts, ": ")
}

// get выполняет GET с токеном; при 401 один раз обновляет токен и повторяет, при 202/429 ждёт и повторяет.
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return nil, err
	}
	refreshed := false
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.BaseURL+path+"?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		body, status, err := c.send(req)
		if err != nil {
			return nil, err
		}
		switch {
		case status == http.StatusOK:
			return body, nil
		case status == http.StatusUnauthorized && !refreshed:
			refreshed = true
			if token, err = c.accessToken(ctx, true); err != nil {
				return nil, err
			}
		case status == http.StatusAccepted && attempt < c.pauses: // выписка формируется
			c.log.Info("sber statement is being prepared, waiting", "attempt", attempt+1)
			if err := c.sleep(ctx, c.pause); err != nil {
				return nil, err
			}
		case status == http.StatusTooManyRequests && attempt < c.pauses:
			if err := c.sleep(ctx, 2*c.pause); err != nil {
				return nil, err
			}
		default:
			return nil, &APIError{Status: status, Message: errorText(body)}
		}
	}
}

// Transactions возвращает все операции счёта за дату (страницы склеиваются) и ответы банка как есть (для аудита).
func (c *Client) Transactions(ctx context.Context, account string, date time.Time) ([]Transaction, []byte, error) {
	var all []Transaction
	var raw bytes.Buffer
	for page := 1; page <= 1000; page++ {
		q := url.Values{"accountNumber": {account}, "statementDate": {date.Format("2006-01-02")}, "page": {strconv.Itoa(page)}}
		body, err := c.get(ctx, "/fintech/api/v2/statement/transactions", q)
		if err != nil {
			return nil, nil, err
		}
		raw.Write(body)
		raw.WriteByte('\n')
		var p struct {
			Transactions []Transaction `json:"transactions"`
			Links        []struct {
				Rel string `json:"rel"`
			} `json:"_links"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, nil, fmt.Errorf("sber api: unreadable statement page %d: %w", page, err)
		}
		all = append(all, p.Transactions...)
		more := false
		for _, l := range p.Links {
			if l.Rel == "next" {
				more = true
			}
		}
		if !more {
			return all, raw.Bytes(), nil
		}
	}
	return nil, nil, errors.New("sber api: too many statement pages")
}
