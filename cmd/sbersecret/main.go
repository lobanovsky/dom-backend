// Команда sbersecret меняет 40-дневный client_secret Sber API на бессрочный и записывает его в файл.
// Банк показывает новое значение один раз, поэтому оно сразу пишется на диск (старое остаётся в <файл>.prev)
// и нигде не печатается.
//
//	go run ./cmd/sbersecret -client-id 92649 -secret private/sber/prod/client_secret \
//	  -p12 private/sber/prod/tls.p12 -p12-pass private/sber/prod/tls.pass -ca certs/sber
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"dom-backend/internal/sberapi"
)

func main() {
	clientID := flag.String("client-id", "", "client_id сервиса Sber API")
	secretFile := flag.String("secret", "", "файл с текущим client_secret (в него же запишем новый)")
	p12 := flag.String("p12", "", "клиентский сертификат .p12")
	p12Pass := flag.String("p12-pass", "", "файл с паролем от .p12")
	caDir := flag.String("ca", "", "каталог с сертификатами Сбера")
	baseURL := flag.String("base-url", sberapi.DefaultBaseURL, "адрес API")
	flag.Parse()
	if *clientID == "" || *secretFile == "" || *p12 == "" || *p12Pass == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*clientID, *secretFile, *p12, *p12Pass, *caDir, *baseURL); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
	fmt.Println("готово: новый client_secret записан в", *secretFile, "(прежний — в", *secretFile+".prev)")
}

func run(clientID, secretFile, p12, p12Pass, caDir, baseURL string) error {
	old, err := os.ReadFile(secretFile)
	if err != nil {
		return err
	}
	httpClient, _, err := sberapi.NewHTTPClient(sberapi.TLSFiles{P12: p12, PasswordFile: p12Pass, CADir: caDir})
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"clientId": clientID, "clientSecret": strings.TrimSpace(string(old))})
	resp, err := httpClient.Post(baseURL+"/fintech/api/applications/secrets/v1/refresh-client-secret", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		var e struct{ Cause, Message string }
		_ = json.Unmarshal(data, &e)
		return fmt.Errorf("банк ответил %d (%s %s); секрет не изменён", resp.StatusCode, e.Cause, e.Message)
	}
	var ok struct {
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(data, &ok); err != nil || ok.ClientSecret == "" {
		return fmt.Errorf("в ответе банка нет clientSecret (статус 200): значение могло быть потеряно, сгенерируйте секрет заново в кабинете")
	}
	// сначала новое значение на диск, потом уже всё остальное
	if err := os.WriteFile(secretFile+".prev", old, 0o600); err != nil {
		return fmt.Errorf("не удалось сохранить прежний секрет (новый секрет: сохраните вручную из файла %s.new): %w", secretFile, writeNew(secretFile+".new", ok.ClientSecret, err))
	}
	return os.WriteFile(secretFile, []byte(ok.ClientSecret), 0o600)
}

func writeNew(path, secret string, orig error) error {
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		return fmt.Errorf("%v; и новый секрет тоже не записан: %w", orig, err)
	}
	return orig
}
