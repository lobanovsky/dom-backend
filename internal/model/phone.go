package model

import (
	"regexp"
	"strings"
)

var (
	nonPhoneChars = regexp.MustCompile(`[^\d\s()+\-.]`)
	nonDigit      = regexp.MustCompile(`\D`)
)

// NormalizePhone приводит телефон к виду для хранения: только цифры, российские номера с кодом страны 7
// («+7 (963) 611-07-26», «8 963 611-07-26», «963 611 07 26» → 79636110726). Остальные номера — только цифры.
// Значение с буквами («доб. 12», «#») не трогаем: из него нельзя безопасно выделить цифры.
// Показывать номер с форматированием — дело интерфейса.
func NormalizePhone(s string) string {
	s = strings.TrimSpace(s)
	if nonPhoneChars.MatchString(s) {
		return s
	}
	digits := nonDigit.ReplaceAllString(s, "")
	plus := strings.HasPrefix(s, "+")
	switch {
	case len(digits) == 11 && digits[0] == '8' && !plus:
		return "7" + digits[1:]
	case len(digits) == 10 && !plus:
		return "7" + digits
	}
	return digits
}
