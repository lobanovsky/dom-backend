package model

import (
	"encoding/json"
	"time"
)

// MailRun — запуск забора реестров из почты.
type MailRun struct {
	ID         int64           `json:"id" db:"id"`
	StartedAt  time.Time       `json:"started_at" db:"started_at"`
	FinishedAt *time.Time      `json:"finished_at" db:"finished_at"`
	Trigger    string          `json:"trigger" db:"trigger"`
	Messages   int             `json:"messages" db:"messages"`
	Files      int             `json:"files" db:"files"`
	Imported   int             `json:"imported" db:"imported"`
	Payments   int             `json:"payments" db:"payments"`
	Duplicates int             `json:"duplicates" db:"duplicates"`
	Failed     int             `json:"failed" db:"failed"`
	Error      *string         `json:"error" db:"error"`
	Details    json.RawMessage `json:"details" db:"details"`
}

// MailMessage — письмо, разобранное сервером.
type MailMessage struct {
	Folder      string
	UIDValidity uint32
	UID         uint32
	MessageID   string
	From        string
	Subject     string
	ReceivedAt  time.Time
	Files       int
}
