package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type HTTPGateAccessSyncer struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHTTPGateAccessSyncer(baseURL, token string) GateAccessSyncer {
	return &HTTPGateAccessSyncer{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: &http.Client{Timeout: 30 * time.Second}}
}

func (s *HTTPGateAccessSyncer) Sync(ctx context.Context, zone, phone, name string, enabled bool) error {
	if s.baseURL == "" || s.token == "" {
		return errors.New("ELDES integration is not configured")
	}
	body, err := json.Marshal(map[string]any{"phone": phone, "name": name, "enabled": enabled})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/"+zone, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ELDES returned HTTP %d", resp.StatusCode)
	}
	return nil
}
