package store

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid")
)

// Error — ошибка с понятным клиенту сообщением; Kind — одна из Err*.
type Error struct {
	Kind error
	Msg  string
}

func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Kind }

func invalid(msg string) error { return &Error{ErrInvalid, msg} }

// mapErr переводит ошибки pgx/PostgreSQL в *Error с подходящим Kind.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return &Error{ErrNotFound, "not found"}
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch {
		case pg.Code == "23505":
			return &Error{ErrConflict, "already exists: " + pg.ConstraintName}
		case pg.Code == "23503":
			return &Error{ErrInvalid, "referenced or dependent record conflict: " + pg.ConstraintName}
		case pg.Code == "23502":
			return &Error{ErrInvalid, "required field is missing: " + pg.ColumnName}
		case pg.Code == "23514":
			return &Error{ErrInvalid, "constraint violated: " + pg.ConstraintName}
		case pg.Code == "P0001": // RAISE EXCEPTION из триггеров целостности (см. миграцию 0003)
			return &Error{ErrInvalid, pg.Message}
		case strings.HasPrefix(pg.Code, "22"):
			return &Error{ErrInvalid, "invalid value: " + pg.Message}
		}
	}
	return err
}
