package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

// decodeJSON читает тело в структуру; неизвестные поля — ошибка.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return false
	}
	return true
}

// listParams — разобранные параметры списка: пагинация и фильтры.
type listParams struct {
	Limit, Offset int
	Deleted       bool // ?deleted=only — показать только удалённые
	ints          map[string]int64
	texts         map[string]string
}

// Int возвращает фильтр-число или nil, если он не задан.
func (p listParams) Int(name string) *int64 {
	if v, ok := p.ints[name]; ok {
		return &v
	}
	return nil
}

// Text возвращает текстовый фильтр или nil, если он не задан.
func (p listParams) Text(name string) *string {
	if v, ok := p.texts[name]; ok {
		return &v
	}
	return nil
}

// parseList разбирает limit/offset и допустимые фильтры; любой другой параметр — 400.
func parseList(w http.ResponseWriter, r *http.Request, intFilters, textFilters []string) (listParams, bool) {
	q := r.URL.Query()
	p := listParams{Limit: defaultLimit, ints: map[string]int64{}, texts: map[string]string{}}
	fail := func(msg string) (listParams, bool) {
		writeError(w, http.StatusBadRequest, msg)
		return p, false
	}
	var err error
	if v := q.Get("limit"); v != "" {
		if p.Limit, err = strconv.Atoi(v); err != nil || p.Limit < 1 || p.Limit > maxLimit {
			return fail(fmt.Sprintf("limit must be between 1 and %d", maxLimit))
		}
	}
	if v := q.Get("offset"); v != "" {
		if p.Offset, err = strconv.Atoi(v); err != nil || p.Offset < 0 {
			return fail("offset must be a non-negative integer")
		}
	}
	known := map[string]bool{"limit": true, "offset": true, "deleted": true}
	switch q.Get("deleted") {
	case "":
	case "only":
		p.Deleted = true
	default:
		return fail(`deleted must be "only"`)
	}
	for _, name := range intFilters {
		known[name] = true
		if v := q.Get(name); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return fail(fmt.Sprintf("%s must be an integer", name))
			}
			p.ints[name] = n
		}
	}
	for _, name := range textFilters {
		known[name] = true
		if v := q.Get(name); v != "" {
			p.texts[name] = v
		}
	}
	if name := unknownParam(q, known); name != "" {
		return fail(fmt.Sprintf("unknown parameter %q", name))
	}
	return p, true
}

func unknownParam(q url.Values, known map[string]bool) string {
	for k := range q {
		if !known[k] {
			return k
		}
	}
	return ""
}

func writeList[T any](w http.ResponseWriter, items []T, p listParams) {
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": p.Limit, "offset": p.Offset})
}

// writeErr переводит ошибки валидации и хранилища в HTTP-ответ.
func writeErr(w http.ResponseWriter, err error) {
	var ve *model.ValidationError
	if errors.As(err, &ve) {
		writeError(w, http.StatusUnprocessableEntity, ve.Error())
		return
	}
	var se *store.Error
	if errors.As(err, &se) {
		switch {
		case errors.Is(se.Kind, store.ErrNotFound):
			writeError(w, http.StatusNotFound, se.Msg)
		case errors.Is(se.Kind, store.ErrConflict):
			writeError(w, http.StatusConflict, se.Msg)
		default:
			writeError(w, http.StatusUnprocessableEntity, se.Msg)
		}
		return
	}
	slog.Error("internal error", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// deletedParam разбирает ?deleted=only у вложенных списков без пагинации.
func deletedParam(w http.ResponseWriter, r *http.Request) (bool, bool) {
	switch r.URL.Query().Get("deleted") {
	case "":
		return false, true
	case "only":
		return true, true
	default:
		writeError(w, http.StatusBadRequest, `deleted must be "only"`)
		return false, false
	}
}
