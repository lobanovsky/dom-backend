package httpapi

import (
	"context"
	"net/http"

	"dom-backend/internal/auth"
	"dom-backend/internal/model"
)

// UserStore — пользователи системы. Физически не удаляются, только отключаются.
type UserStore interface {
	List(ctx context.Context) ([]model.User, error)
	Create(ctx context.Context, username, passwordHash, role string) (model.User, error)
	Update(ctx context.Context, id int64, role string, active bool) (model.User, error)
	SetPassword(ctx context.Context, id int64, passwordHash string) (model.User, error)
}

type userHandlers struct {
	s    UserStore
	auth *authHandlers
}

func (h userHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/users", h.list)
	mux.HandleFunc("POST /api/v1/users", h.create)
	mux.HandleFunc("PUT /api/v1/users/{id}", h.update)
}

func (h userHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, ok := parseList(w, r, nil, nil)
	if !ok {
		return
	}
	items, err := h.s.List(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h userHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := firstValidation(model.ValidateUsername(in.Username), model.ValidateRole(in.Role), model.ValidatePassword("password", in.Password)); err != nil {
		writeErr(w, err)
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	u, err := h.s.Create(r.Context(), in.Username, hash, in.Role)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

// update: роль и активность обязательны (полная замена, как у остальных сущностей); password — необязательный сброс пароля.
func (h userHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Role     string `json:"role"`
		Active   *bool  `json:"active"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Active == nil {
		writeError(w, http.StatusUnprocessableEntity, "active: is required")
		return
	}
	if err := model.ValidateRole(in.Role); err != nil {
		writeErr(w, err)
		return
	}
	var hash string
	if in.Password != "" {
		if err := model.ValidatePassword("password", in.Password); err != nil {
			writeErr(w, err)
			return
		}
		var err error
		if hash, err = auth.HashPassword(in.Password); err != nil {
			writeErr(w, err)
			return
		}
	}
	u, err := h.s.Update(r.Context(), id, in.Role, *in.Active)
	if err != nil {
		writeErr(w, err)
		return
	}
	if hash != "" {
		if u, err = h.s.SetPassword(r.Context(), id, hash); err != nil {
			writeErr(w, err)
			return
		}
		// Админ сменил пароль себе: прежняя сессия завершилась, выдаём новую.
		if currentUser(r).ID == id {
			h.auth.setSession(w, r, u)
		}
	}
	writeJSON(w, http.StatusOK, u)
}

func firstValidation(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
