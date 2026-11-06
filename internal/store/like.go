package store

import "strings"

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// likePattern превращает пользовательский текст в шаблон ILIKE «содержит»;
// спецсимволы LIKE экранируются. nil или пустая строка — фильтр не задан.
func likePattern(q *string) *string {
	if q == nil || strings.TrimSpace(*q) == "" {
		return nil
	}
	p := "%" + likeEscaper.Replace(strings.TrimSpace(*q)) + "%"
	return &p
}

// prefixPattern — шаблон LIKE «начинается с»; nil или пустая строка — фильтр не задан.
func prefixPattern(q *string) *string {
	if q == nil || strings.TrimSpace(*q) == "" {
		return nil
	}
	p := likeEscaper.Replace(strings.TrimSpace(*q)) + "%"
	return &p
}

// exactPattern — шаблон ILIKE «совпадает целиком» (спецсимволы экранируются); nil или пустая строка — nil.
func exactPattern(q *string) *string {
	if q == nil || strings.TrimSpace(*q) == "" {
		return nil
	}
	p := likeEscaper.Replace(strings.TrimSpace(*q))
	return &p
}

// wordPatterns разбивает пользовательский текст на слова и делает из каждого шаблон ILIKE «содержит»;
// пустой запрос — пустой список (не nil: параметр text[] в SQL не должен быть NULL).
func wordPatterns(q *string) []string {
	out := []string{}
	if q == nil {
		return out
	}
	for _, w := range strings.Fields(*q) {
		out = append(out, "%"+likeEscaper.Replace(w)+"%")
	}
	return out
}
