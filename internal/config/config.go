package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL        string
	ListenAddr         string
	AdminUsername      string
	AdminPasswordHash  string
	AdminSessionSecret string
	SessionTTL         time.Duration
	Sber               Sber
}

// Sber — настройки получения выписок из Sber API. Пустой ClientID = интеграция выключена.
type Sber struct {
	BaseURL          string
	ClientID         string
	ClientSecretFile string
	TLSP12           string
	TLSP12PassFile   string
	CADir            string
	SyncInterval     time.Duration // 0 = опрос по расписанию выключен, остаётся кнопка
	SyncDays         int
}

func (s Sber) Enabled() bool { return s.ClientID != "" }

func Load() (Config, error) {
	ttl, err := duration("SESSION_TTL", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	interval, err := duration("SBER_SYNC_INTERVAL", time.Hour)
	if err != nil {
		return Config{}, err
	}
	days := 3
	if v := os.Getenv("SBER_SYNC_DAYS"); v != "" {
		if days, err = strconv.Atoi(v); err != nil || days < 1 || days > 30 {
			return Config{}, fmt.Errorf("SBER_SYNC_DAYS: want a number from 1 to 30")
		}
	}
	c := Config{
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		ListenAddr:         get("LISTEN_ADDR", ":8080"),
		AdminUsername:      os.Getenv("ADMIN_USERNAME"),
		AdminPasswordHash:  os.Getenv("ADMIN_PASSWORD_HASH"),
		AdminSessionSecret: os.Getenv("ADMIN_SESSION_SECRET"),
		SessionTTL:         ttl,
		Sber: Sber{
			BaseURL: os.Getenv("SBER_BASE_URL"), ClientID: os.Getenv("SBER_CLIENT_ID"),
			ClientSecretFile: os.Getenv("SBER_CLIENT_SECRET_FILE"), TLSP12: os.Getenv("SBER_TLS_P12"),
			TLSP12PassFile: os.Getenv("SBER_TLS_P12_PASSWORD_FILE"), CADir: os.Getenv("SBER_CA_DIR"),
			SyncInterval: interval, SyncDays: days,
		},
	}
	return c, c.validate()
}

func (c Config) validate() error {
	var missing []string
	for name, v := range map[string]string{
		"DATABASE_URL":         c.DatabaseURL,
		"ADMIN_USERNAME":       c.AdminUsername,
		"ADMIN_PASSWORD_HASH":  c.AdminPasswordHash,
		"ADMIN_SESSION_SECRET": c.AdminSessionSecret,
	} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if c.Sber.Enabled() {
		for name, v := range map[string]string{
			"SBER_CLIENT_SECRET_FILE":    c.Sber.ClientSecretFile,
			"SBER_TLS_P12":               c.Sber.TLSP12,
			"SBER_TLS_P12_PASSWORD_FILE": c.Sber.TLSP12PassFile,
		} {
			if v == "" {
				missing = append(missing, name+" (required with SBER_CLIENT_ID)")
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	return nil
}

func get(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func duration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}
