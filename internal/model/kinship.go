package model

// Kinship — справочник родства проживающего с собственником (нужен, в частности, для расчёта льгот).
// Код (латиница) хранится в residencies.relation и после создания не меняется; название и порядок можно править.
type Kinship struct {
	Meta
	Code     string `json:"code" db:"code"`
	Name     string `json:"name" db:"name"`
	Position int    `json:"position" db:"position"`
}

func (k Kinship) Validate() error {
	return firstErr(kindCode("code", k.Code), required("name", k.Name))
}

type KinshipFilter struct {
	Deleted bool
}
