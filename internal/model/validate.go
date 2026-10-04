package model

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// ValidationError — нарушено бизнес-правило входных данных; API отвечает 422.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Msg }

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Msg: fmt.Sprintf(format, args...)}
}

func required(field, v string) error {
	if strings.TrimSpace(v) == "" {
		return invalid(field, "is required")
	}
	return nil
}

func oneOf(field, v string, allowed []string) error {
	if !slices.Contains(allowed, v) {
		return invalid(field, "must be one of: %s", strings.Join(allowed, ", "))
	}
	return nil
}

func positive(field string, v *float64) error {
	if v != nil && *v <= 0 {
		return invalid(field, "must be positive")
	}
	return nil
}

func positiveInt(field string, v *int) error {
	if v != nil && *v <= 0 {
		return invalid(field, "must be positive")
	}
	return nil
}

func period(from Date, to *Date) error {
	if from.IsZero() {
		return invalid("valid_from", "is required")
	}
	if to != nil && to.Before(from.Time) {
		return invalid("valid_to", "must not be before valid_from")
	}
	return nil
}

// exactlyOne проверяет, что задан ровно один владелец: физлицо или юрлицо.
func exactlyOne(personID, legalEntityID *int64) error {
	if (personID == nil) == (legalEntityID == nil) {
		return invalid("person_id", "exactly one of person_id and legal_entity_id must be set")
	}
	return nil
}

// firstErr возвращает первую непустую ошибку.
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// Meta — служебные поля, которые выставляет сервер.
type Meta struct {
	ID        int64     `json:"id" db:"id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
