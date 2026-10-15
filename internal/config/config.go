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
	AdminUsername      string // необязательны: нужны только чтобы создать первого администратора, пока в БД нет пользователей
	AdminPasswordHash  string
	AdminSessionSecret string
	SessionTTL         time.Duration
	Sber               Sber
	Mail               Mail
}

// Mail — настройки забора реестров из почты по IMAP. Пустой User = интеграция выключена.
type Mail struct {
	User         string
	PasswordFile string        // пароль для внешнего приложения (не обычный пароль почты)
	Addr         string        // host:port, IMAP поверх TLS
	Folder       string        // папка с письмами-реестрами
	From         string        // необязательный фильтр по отправителю
	SyncEvery    time.Duration // 0 = только по кнопке
	SinceDays    int           // глубина поиска писем
}

func (m Mail) Enabled() bool { return m.User != "" }

// Sber — настройки получения выписок из Sber API. Пустой ClientID = интеграция выключена.
type Sber struct {
	BaseURL          string
	ClientID         string
	ClientSecretFile string
	TLSP12           string
	TLSP12PassFile   string
	CADir            string
	SyncAt           string         // «ЧЧ:ММ» — время ежедневного опроса; пусто = только по кнопке
	SyncLocation     *time.Location // часовой пояс SyncAt
	SyncDays         int
}

func (s Sber) Enabled() bool { return s.ClientID != "" }

func Load() (Config, error) {
	ttl, err := duration("SESSION_TTL", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	syncAt := get("SBER_SYNC_AT", "01:00")
	if syncAt == "off" {
		syncAt = ""
	}
	if syncAt != "" {
		if _, err := time.Parse("15:04", syncAt); err != nil {
			return Config{}, fmt.Errorf("SBER_SYNC_AT: want HH:MM (for example 01:00) or off")
		}
	}
	loc, err := time.LoadLocation(get("SBER_SYNC_TZ", "Europe/Moscow"))
	if err != nil {
		return Config{}, fmt.Errorf("SBER_SYNC_TZ: %w", err)
	}
	days := 3
	if v := os.Getenv("SBER_SYNC_DAYS"); v != "" {
		if days, err = strconv.Atoi(v); err != nil || days < 1 || days > 30 {
			return Config{}, fmt.Errorf("SBER_SYNC_DAYS: want a number from 1 to 30")
		}
	}
	mail := Mail{
		User: os.Getenv("MAIL_USER"), PasswordFile: os.Getenv("MAIL_PASSWORD_FILE"), Addr: get("MAIL_IMAP_ADDR", "imap.mail.ru:993"),
		Folder: get("MAIL_FOLDER", "INBOX"), From: os.Getenv("MAIL_FROM"), SinceDays: 30,
	}
	if v := get("MAIL_SYNC_EVERY", "24h"); v != "off" {
		if mail.SyncEvery, err = time.ParseDuration(v); err != nil || mail.SyncEvery < time.Minute {
			return Config{}, fmt.Errorf("MAIL_SYNC_EVERY: want a duration of at least 1m (for example 24h) or off")
		}
	}
	if v := os.Getenv("MAIL_SINCE_DAYS"); v != "" {
		if mail.SinceDays, err = strconv.Atoi(v); err != nil || mail.SinceDays < 1 || mail.SinceDays > 365 {
			return Config{}, fmt.Errorf("MAIL_SINCE_DAYS: want a number from 1 to 365")
		}
	}
	c := Config{
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		ListenAddr:         get("LISTEN_ADDR", ":8080"),
		AdminUsername:      os.Getenv("ADMIN_USERNAME"),
		AdminPasswordHash:  os.Getenv("ADMIN_PASSWORD_HASH"),
		AdminSessionSecret: os.Getenv("ADMIN_SESSION_SECRET"),
		SessionTTL:         ttl,
		Mail:               mail,
		Sber: Sber{
			BaseURL: os.Getenv("SBER_BASE_URL"), ClientID: os.Getenv("SBER_CLIENT_ID"),
			ClientSecretFile: os.Getenv("SBER_CLIENT_SECRET_FILE"), TLSP12: os.Getenv("SBER_TLS_P12"),
			TLSP12PassFile: os.Getenv("SBER_TLS_P12_PASSWORD_FILE"), CADir: os.Getenv("SBER_CA_DIR"),
			SyncAt: syncAt, SyncLocation: loc, SyncDays: days,
		},
	}
	return c, c.validate()
}

func (c Config) validate() error {
	var missing []string
	for name, v := range map[string]string{
		"DATABASE_URL":         c.DatabaseURL,
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
	if c.Mail.Enabled() && c.Mail.PasswordFile == "" {
		missing = append(missing, "MAIL_PASSWORD_FILE (required with MAIL_USER)")
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
