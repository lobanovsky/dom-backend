package model

import (
	"strings"
	"time"
	"unicode"
)

const (
	RoleAdmin  = "admin"  // полный доступ и управление пользователями
	RoleViewer = "viewer" // только просмотр
)

var userRoles = []string{RoleAdmin, RoleViewer}

const (
	minPasswordLen = 8
	maxPasswordLen = 72 // предел bcrypt в байтах
)

// User — пользователь системы. Хеш пароля и версия токена наружу не отдаются.
type User struct {
	ID           int64     `json:"id" db:"id"`
	Username     string    `json:"username" db:"username"`
	Role         string    `json:"role" db:"role"`
	Active       bool      `json:"active" db:"active"`
	PasswordHash string    `json:"-" db:"password_hash"`
	TokenVersion int       `json:"-" db:"token_version"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

// ValidateUsername: 3-50 символов без пробелов и управляющих знаков.
func ValidateUsername(username string) error {
	n := len([]rune(username))
	if n < 3 || n > 50 {
		return invalid("username", "must be 3-50 characters")
	}
	if strings.IndexFunc(username, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return invalid("username", "must not contain spaces")
	}
	return nil
}

func ValidateRole(role string) error { return oneOf("role", role, userRoles) }

// ValidatePassword: не короче 8 символов и не длиннее 72 байт (предел bcrypt).
func ValidatePassword(field, password string) error {
	if len([]rune(password)) < minPasswordLen {
		return invalid(field, "must be at least %d characters", minPasswordLen)
	}
	if len(password) > maxPasswordLen {
		return invalid(field, "must be at most %d bytes", maxPasswordLen)
	}
	return nil
}
