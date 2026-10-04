package model

import (
	"net/mail"
	"slices"
	"strings"
	"unicode"
)

const (
	maxContacts   = 10
	maxContactLen = 64
	minPhoneDigit = 5
)

// Person — физлицо: собственник, житель или плательщик.
// Телефонов и email может быть несколько; первый в списке — основной.
type Person struct {
	Meta
	LastName   string   `json:"last_name" db:"last_name"`
	FirstName  string   `json:"first_name" db:"first_name"`
	MiddleName *string  `json:"middle_name" db:"middle_name"`
	BirthDate  *Date    `json:"birth_date" db:"birth_date"`
	Phones     []string `json:"phones" db:"phones"`
	Emails     []string `json:"emails" db:"emails"`
}

// Normalize приводит контакты к каноничному виду: обрезает пробелы, убирает
// пустые и повторяющиеся значения (порядок сохраняется) и заменяет nil на пустой срез.
func (p *Person) Normalize() {
	p.Phones = normalizeList(p.Phones)
	p.Emails = normalizeList(p.Emails)
}

func normalizeList(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || slices.Contains(out, item) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (p Person) Validate() error {
	return firstErr(
		required("last_name", p.LastName),
		required("first_name", p.FirstName),
		validateList("phones", p.Phones, validPhone, "must contain at least 5 digits"),
		validateList("emails", p.Emails, validEmail, "is not a valid email"),
	)
}

func validateList(field string, items []string, valid func(string) bool, reason string) error {
	if len(items) > maxContacts {
		return invalid(field, "must contain at most %d items", maxContacts)
	}
	for _, item := range items {
		if len(item) > maxContactLen {
			return invalid(field, "item is too long (max %d characters)", maxContactLen)
		}
		if !valid(item) {
			return invalid(field, "%q: %s", item, reason)
		}
	}
	return nil
}

// validPhone: формат не навязываем (+7, 8, скобки, «доб.»), но в номере должно быть
// достаточно цифр, чтобы это был телефон, а не случайный текст.
func validPhone(s string) bool {
	digits := 0
	for _, r := range s {
		if unicode.IsDigit(r) {
			digits++
		}
	}
	return digits >= minPhoneDigit
}

// validEmail принимает только голый адрес: «Имя <a@b.ru>» не подходит.
func validEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}

// LegalEntity — юрлицо-собственник (например, коммерческих помещений).
type LegalEntity struct {
	Meta
	Name string  `json:"name" db:"name"`
	INN  string  `json:"inn" db:"inn"`
	KPP  *string `json:"kpp" db:"kpp"`
	OGRN *string `json:"ogrn" db:"ogrn"`
}

func (l LegalEntity) Validate() error {
	return firstErr(required("name", l.Name), required("inn", l.INN))
}
