package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthenticated    = errors.New("authentication required")
)

// Users — откуда берутся пользователи (в проде store.Users).
type Users interface {
	ByUsername(ctx context.Context, username string) (model.User, error)
	ByID(ctx context.Context, id int64) (model.User, error)
}

// Auth проверяет пароли и выдаёт/проверяет подписанные токены сессии.
// Токен хранит только id пользователя и версию: роль и активность берутся из БД на каждый запрос,
// поэтому отключение и смена роли действуют сразу.
type Auth struct {
	users  Users
	secret []byte
	ttl    time.Duration
	now    func() time.Time

	dummyOnce sync.Once
	dummyHash []byte
}

func New(users Users, secret string, ttl time.Duration) *Auth {
	return &Auth{users: users, secret: []byte(secret), ttl: ttl, now: time.Now}
}

func (a *Auth) TTL() time.Duration { return a.ttl }

func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(h), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Login проверяет логин и пароль. Для неизвестного логина тоже сверяется хеш, чтобы время ответа
// не выдавало, существует ли такой пользователь. Отключённый пользователь не входит.
func (a *Auth) Login(ctx context.Context, username, password string) (model.User, error) {
	u, err := a.users.ByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.dummyOnce.Do(func() { a.dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy"), bcrypt.DefaultCost) })
			_ = bcrypt.CompareHashAndPassword(a.dummyHash, []byte(password))
			return model.User{}, ErrInvalidCredentials
		}
		return model.User{}, err
	}
	if !CheckPassword(u.PasswordHash, password) || !u.Active {
		return model.User{}, ErrInvalidCredentials
	}
	return u, nil
}

// Issue возвращает токен вида base64(id).версия.срок.подпись.
func (a *Auth) Issue(u model.User) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(u.ID, 10))) + "." +
		strconv.Itoa(u.TokenVersion) + "." + strconv.FormatInt(a.now().Add(a.ttl).Unix(), 10)
	return payload + "." + a.sign(payload)
}

// Authenticate проверяет подпись и срок токена и возвращает действующего пользователя.
// Ошибка ErrUnauthenticated — токен негоден (подпись, срок, пользователь отключён или версия устарела);
// любая другая — сбой хранилища.
func (a *Auth) Authenticate(ctx context.Context, token string) (model.User, error) {
	i := strings.LastIndexByte(token, '.')
	if i < 0 {
		return model.User{}, ErrUnauthenticated
	}
	payload, sig := token[:i], token[i+1:]
	if !hmac.Equal([]byte(sig), []byte(a.sign(payload))) {
		return model.User{}, ErrUnauthenticated
	}
	parts := strings.Split(payload, ".")
	if len(parts) != 3 {
		return model.User{}, ErrUnauthenticated
	}
	rawID, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return model.User{}, ErrUnauthenticated
	}
	id, errID := strconv.ParseInt(string(rawID), 10, 64)
	version, errVer := strconv.Atoi(parts[1])
	exp, errExp := strconv.ParseInt(parts[2], 10, 64)
	if errID != nil || errVer != nil || errExp != nil || a.now().Unix() >= exp {
		return model.User{}, ErrUnauthenticated
	}
	u, err := a.users.ByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return model.User{}, ErrUnauthenticated
	}
	if err != nil {
		return model.User{}, err
	}
	if !u.Active || u.TokenVersion != version {
		return model.User{}, ErrUnauthenticated
	}
	return u, nil
}

func (a *Auth) sign(payload string) string {
	m := hmac.New(sha256.New, a.secret)
	m.Write([]byte(payload)) //nolint:errcheck // hash.Hash.Write never returns an error
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
