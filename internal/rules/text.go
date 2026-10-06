// Package rules — движок правил определения лицевого счёта входящего платежа: проверка условий, действия,
// сопоставление ФИО и номеров помещений. Не обращается к БД: справочники передаются в Index.
package rules

import (
	"strings"
	"unicode"
)

// NormText приводит текст к виду для сравнения: регистр, «ё», лишние пробелы.
func NormText(s string) string {
	s = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, "Ё", "е"), "ё", "е"))
	return strings.Join(strings.Fields(s), " ")
}

// noSpaces убирает все пробельные символы (для сравнения номеров вида «0 0 0 0 5 0 0 1 0 7»).
func noSpaces(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// Tokens разбивает ФИО на слова (буквы, цифры и дефис), нормализует и сортирует: порядок слов не важен.
func Tokens(s string) []string {
	words := strings.FieldsFunc(NormText(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' })
	for i := 1; i < len(words); i++ { // сортировка вставками: слов мало
		for j := i; j > 0 && words[j] < words[j-1]; j-- {
			words[j], words[j-1] = words[j-1], words[j]
		}
	}
	return words
}

// NamesMatch: одинаковое число слов и каждое слово плательщика совпадает со словом собственника или является его началом
// (банк обрезает длинные ФИО: «КОПЫЛОВА СВЕТЛАНА ГЕННАДЬ»). Слов должно быть не меньше двух.
func NamesMatch(payer, owner []string) bool {
	if len(payer) < 2 || len(payer) != len(owner) {
		return false
	}
	for i := range payer {
		if !strings.HasPrefix(owner[i], payer[i]) {
			return false
		}
	}
	return true
}
