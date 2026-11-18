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
	"unicode"
)

type HTTPGateAccessSyncer struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHTTPGateAccessSyncer(baseURL, token string) GateAccessSyncer {
	return &HTTPGateAccessSyncer{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: &http.Client{Timeout: 30 * time.Second}}
}

var cyrillicGateName = map[rune]string{
	'А': "A", 'а': "A", 'Б': "B", 'б': "B", 'В': "V", 'в': "V", 'Г': "G", 'г': "G",
	'Д': "D", 'д': "D", 'Е': "E", 'е': "E", 'Ё': "E", 'ё': "E", 'Ж': "Zh", 'ж': "Zh",
	'З': "Z", 'з': "Z", 'И': "I", 'и': "I", 'Й': "I", 'й': "I", 'К': "K", 'к': "K",
	'Л': "L", 'л': "L", 'М': "M", 'м': "M", 'Н': "N", 'н': "N", 'О': "O", 'о': "O",
	'П': "P", 'п': "P", 'Р': "R", 'р': "R", 'С': "S", 'с': "S", 'Т': "T", 'т': "T",
	'У': "U", 'у': "U", 'Ф': "F", 'ф': "F", 'Х': "H", 'х': "H", 'Ц': "Ts", 'ц': "Ts",
	'Ч': "Ch", 'ч': "Ch", 'Ш': "Sh", 'ш': "Sh", 'Щ': "Shch", 'щ': "Shch", 'Ы': "Y", 'ы': "Y",
	'Э': "E", 'э': "E", 'Ю': "Yu", 'ю': "Yu", 'Я': "Ya", 'я': "Ya",
}

func gateDeviceName(value string) string {
	var result strings.Builder
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			result.WriteRune(r)
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '/' || r == '.':
			result.WriteByte(' ')
		default:
			if latin, ok := cyrillicGateName[r]; ok {
				result.WriteString(latin)
			} else {
				result.WriteByte(' ')
			}
		}
	}
	name := strings.Join(strings.Fields(result.String()), " ")
	if len(name) > 15 {
		name = name[:15]
	}
	return name
}

// gatePremisesLabel оставляет в подписи номер помещения и столько имени пользователя, сколько помещается в лимит ELDES.
func gatePremisesLabel(premisesNumber, recipientName string) string {
	return gateDeviceName(strings.TrimSpace(premisesNumber) + "-" + strings.TrimSpace(recipientName))
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
