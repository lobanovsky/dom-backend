package model

import (
	"regexp"
	"strings"
	"unicode"
)

// BankAccount — расчётный счёт организации (УК, ТСН/ТСЖ): обычный или специальный (капремонт).
// Активность не хранится, а вычисляется по периоду действия: Active — сервер, при записи игнорируется.
type BankAccount struct {
	Meta
	OrganizationID int64    `json:"organization_id" db:"organization_id"`
	Number         string   `json:"number" db:"number"`
	BIK            *string  `json:"bik" db:"bik"`
	BankName       *string  `json:"bank_name" db:"bank_name"`
	IsSpecial      bool     `json:"is_special" db:"is_special"`
	Description    *string  `json:"description" db:"description"`
	RegistryCodes  []string `json:"registry_codes" db:"registry_codes"` // коды реестра в имени файла (VTB_231068_001), если банк не пишет туда номер счёта
	ValidFrom      Date     `json:"valid_from" db:"valid_from"`
	ValidTo        *Date    `json:"valid_to" db:"valid_to"`
	Active         bool     `json:"active" db:"active"`
}

func (a BankAccount) Validate() error {
	return firstErr(
		positiveInt("organization_id", intPtr(a.OrganizationID)),
		digits("number", a.Number, 20),
		optionalDigits("bik", a.BIK, 9),
		registryCodes(a.RegistryCodes),
		period(a.ValidFrom, a.ValidTo),
	)
}

const maxRegistryCodes = 20

var registryCodeRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func registryCodes(codes []string) error {
	if len(codes) > maxRegistryCodes {
		return invalid("registry_codes", "must contain at most %d items", maxRegistryCodes)
	}
	for _, c := range codes {
		if !registryCodeRe.MatchString(c) {
			return invalid("registry_codes", "must be 1-64 latin letters, digits, _ or -")
		}
	}
	return nil
}

// SetDefaults убирает пустые и повторяющиеся (без учёта регистра) коды; список никогда не nil: в БД он NOT NULL.
func (a *BankAccount) SetDefaults() {
	out := make([]string, 0, len(a.RegistryCodes))
	seen := map[string]bool{}
	for _, c := range a.RegistryCodes {
		c = strings.TrimSpace(c)
		if k := strings.ToLower(c); c != "" && !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	a.RegistryCodes = out
}

// digits проверяет, что значение состоит ровно из n цифр.
func digits(field, v string, n int) error {
	if err := required(field, v); err != nil {
		return err
	}
	if len(v) != n || strings.IndexFunc(v, func(r rune) bool { return !unicode.IsDigit(r) }) >= 0 {
		return invalid(field, "must contain exactly %d digits", n)
	}
	return nil
}

func optionalDigits(field string, v *string, n int) error {
	if v == nil || *v == "" {
		return nil
	}
	return digits(field, *v, n)
}

// optionalINN: у юрлица 10 цифр, у физлица и ИП 12.
func optionalINN(field string, v *string) error {
	if v == nil || *v == "" {
		return nil
	}
	if digits(field, *v, 10) != nil && digits(field, *v, 12) != nil {
		return invalid(field, "must contain 10 or 12 digits")
	}
	return nil
}
