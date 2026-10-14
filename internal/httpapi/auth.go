package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"dom-backend/internal/auth"
	"dom-backend/internal/model"
)

const sessionCookie = "dom_session"

type userCtxKey struct{}

// currentUser — пользователь сессии; кладётся в контекст middleware require.
func currentUser(r *http.Request) model.User {
	u, _ := r.Context().Value(userCtxKey{}).(model.User)
	return u
}

type authHandlers struct {
	auth  *auth.Auth
	users UserStore
}

func (h *authHandlers) setSession(w http.ResponseWriter, r *http.Request, u model.User) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    h.auth.Issue(u),
		Path:     "/",
		MaxAge:   int(h.auth.TTL().Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *authHandlers) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	u, err := h.auth.Login(r.Context(), req.Username, req.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	h.setSession(w, r, u)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *authHandlers) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// me отвечает текущим пользователем; вызывается за require, поэтому сессия уже проверена.
func (h *authHandlers) me(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "username": u.Username, "role": u.Role})
}

// changePassword меняет пароль самого пользователя (любой роли). Прежние сессии завершаются, текущая перевыдаётся.
func (h *authHandlers) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := currentUser(r)
	if !auth.CheckPassword(u.PasswordHash, req.Current) {
		writeError(w, http.StatusUnprocessableEntity, "current_password: is incorrect")
		return
	}
	if err := model.ValidatePassword("new_password", req.New); err != nil {
		writeErr(w, err)
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		writeErr(w, err)
		return
	}
	updated, err := h.users.SetPassword(r.Context(), u.ID, hash)
	if err != nil {
		writeErr(w, err)
		return
	}
	h.setSession(w, r, updated)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// require пропускает только запросы с действующей сессией и правом на метод/путь (см. allowed).
func (h *authHandlers) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		u, err := h.auth.Authenticate(r.Context(), c.Value)
		if errors.Is(err, auth.ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if err != nil {
			slog.Error("authenticate", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !allowed(u, r.Method, r.URL.Path) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey{}, u)))
	})
}

// allowed — единое правило доступа, по умолчанию «запрещено»:
//   - admin может всё;
//   - остальные (viewer) только читают (GET/HEAD) и меняют собственный пароль;
//   - управление пользователями — только admin, даже чтение.
//
// Все GET-маршруты API только читают; всё, что что-то меняет (импорт, определение счетов, синхронизация), — POST/PUT/DELETE.
func allowed(u model.User, method, path string) bool {
	if u.IsAdmin() {
		return true
	}
	if path == "/api/v1/users" || strings.HasPrefix(path, "/api/v1/users/") {
		return false
	}
	if path == "/api/v1/auth/password" && method == http.MethodPut {
		return true
	}
	return method == http.MethodGet || method == http.MethodHead
}
