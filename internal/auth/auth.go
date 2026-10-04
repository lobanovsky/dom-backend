package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Auth проверяет админа и выдаёт/проверяет подписанные токены сессии.
type Auth struct {
	username string
	hash     []byte
	secret   []byte
	ttl      time.Duration
	now      func() time.Time
}

func New(username, passwordHash, secret string, ttl time.Duration) *Auth {
	return &Auth{username: username, hash: []byte(passwordHash), secret: []byte(secret), ttl: ttl, now: time.Now}
}

func (a *Auth) TTL() time.Duration { return a.ttl }

func (a *Auth) Username() string { return a.username }

// Check сверяет логин и пароль; время работы не зависит от того, неверен логин или пароль.
func (a *Auth) Check(username, password string) bool {
	passOK := bcrypt.CompareHashAndPassword(a.hash, []byte(password)) == nil
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(a.username)) == 1
	return passOK && userOK
}

// Issue возвращает токен вида base64(user).exp.sig.
func (a *Auth) Issue() string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(a.username)) + "." +
		strconv.FormatInt(a.now().Add(a.ttl).Unix(), 10)
	return payload + "." + a.sign(payload)
}

// Verify проверяет подпись и срок действия токена.
func (a *Auth) Verify(token string) bool {
	i := strings.LastIndexByte(token, '.')
	if i < 0 {
		return false
	}
	payload, sig := token[:i], token[i+1:]
	if !hmac.Equal([]byte(sig), []byte(a.sign(payload))) {
		return false
	}
	parts := strings.Split(payload, ".")
	if len(parts) != 2 {
		return false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	return err == nil && a.now().Unix() < exp
}

func (a *Auth) sign(payload string) string {
	m := hmac.New(sha256.New, a.secret)
	m.Write([]byte(payload)) //nolint:errcheck // hash.Hash.Write never returns an error
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
