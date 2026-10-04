package config

import (
	"fmt"
	"os"
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
}

func Load() (Config, error) {
	ttl, err := duration("SESSION_TTL", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	c := Config{
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		ListenAddr:         get("LISTEN_ADDR", ":8080"),
		AdminUsername:      os.Getenv("ADMIN_USERNAME"),
		AdminPasswordHash:  os.Getenv("ADMIN_PASSWORD_HASH"),
		AdminSessionSecret: os.Getenv("ADMIN_SESSION_SECRET"),
		SessionTTL:         ttl,
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
