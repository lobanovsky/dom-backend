package model

import "time"

// SberRun — запуск получения выписок из Sber API.
type SberRun struct {
	ID         int64      `json:"id" db:"id"`
	StartedAt  time.Time  `json:"started_at" db:"started_at"`
	FinishedAt *time.Time `json:"finished_at" db:"finished_at"`
	Trigger    string     `json:"trigger" db:"trigger"`
	DateFrom   Date       `json:"date_from" db:"date_from"`
	DateTo     Date       `json:"date_to" db:"date_to"`
	Accounts   int        `json:"accounts" db:"accounts"`
	Incoming   int        `json:"incoming" db:"incoming"`
	Outgoing   int        `json:"outgoing" db:"outgoing"`
	Skipped    int        `json:"skipped" db:"skipped"`
	Error      *string    `json:"error" db:"error"`
}

// SberTokens — пара токенов доступа. RefreshToken при каждом обновлении меняется.
type SberTokens struct {
	AccessToken     string
	RefreshToken    string
	AccessExpiresAt time.Time
}
