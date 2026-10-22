package model

import "regexp"

// PremisesKind — справочник видов помещений. Код (латиница) хранится в premises.kind и
// services.premises_kinds, после создания не меняется; название и порядок можно править.
type PremisesKind struct {
	Meta
	Code     string `json:"code" db:"code"`
	Name     string `json:"name" db:"name"`
	Position int    `json:"position" db:"position"`
}

var kindCodeRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// kindCode проверяет только вид кода; существование вида проверяют триггеры БД.
func kindCode(field, v string) error {
	if !kindCodeRe.MatchString(v) {
		return invalid(field, "must be a code of 1 to 40 latin lowercase letters, digits or underscores, starting with a letter")
	}
	return nil
}

// ValidKindCode сообщает, похожа ли строка на код вида помещения.
func ValidKindCode(v string) bool { return kindCodeRe.MatchString(v) }

func (k PremisesKind) Validate() error {
	return firstErr(kindCode("code", k.Code), required("name", k.Name))
}

type PremisesKindFilter struct {
	Deleted bool
}
