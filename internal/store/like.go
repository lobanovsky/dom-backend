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
