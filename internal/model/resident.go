package model

// ResidentAccount — отдельная от сотрудников учётная запись собственника.
type ResidentAccount struct {
	ID           int64  `json:"id" db:"id"`
	PersonID     int64  `json:"person_id" db:"person_id"`
	Email        string `json:"email" db:"email"`
	PasswordHash string `json:"-" db:"password_hash"`
}

type ResidentProperty struct {
	Premises
	Address  string `json:"address" db:"address"`
	KindName string `json:"kind_name" db:"kind_name"`
	ShareNum int    `json:"share_num" db:"share_num"`
	ShareDen int    `json:"share_den" db:"share_den"`
}

type ResidentWindow struct {
	BuildingID int64 `json:"building_id" db:"building_id"`
	Period     Date  `json:"period" db:"period"`
	OpensOn    Date  `json:"opens_on" db:"opens_on"`
	ClosesOn   Date  `json:"closes_on" db:"closes_on"`
}

type ResidentReading struct {
	MeterReading
	AuthorID *int64 `json:"-" db:"author_id"`
	Revision int64  `json:"revision" db:"revision"`
	Editable bool   `json:"editable" db:"editable"`
}
