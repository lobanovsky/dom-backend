package model

import (
	"strings"
	"unicode"
)

// BankAccount — расчётный счёт организации (УК, ТСН/ТСЖ): обычный или специальный (капремонт).
// Активность не хранится, а вычисляется по периоду действия: Active — сервер, при записи игнорируется.
type BankAccount struct {
	Meta
	OrganizationID int64   `json:"organization_id" db:"organization_id"`
	Number         string  `json:"number" db:"number"`
	BIK            *string `json:"bik" db:"bik"`
	BankName       *string `json:"bank_name" db:"bank_name"`
	IsSpecial      bool    `json:"is_special" db:"is_special"`
	Description    *string `json:"description" db:"description"`
	ValidFrom      Date    `json:"valid_from" db:"valid_from"`
	ValidTo        *Date   `json:"valid_to" db:"valid_to"`
	Active         bool    `json:"active" db:"active"`
}

func (a BankAccount) Validate() error {
	return firstErr(
		positiveInt("organization_id", intPtr(a.OrganizationID)),
		digits("number", a.Number, 20),
		optionalDigits("bik", a.BIK, 9),
		period(a.ValidFrom, a.ValidTo),
	)
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
