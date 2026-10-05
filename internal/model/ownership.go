package model

// Ownership — право собственности на помещение: доля и период владения.
// Владелец — физлицо или юрлицо (ровно одно из полей).
type Ownership struct {
	Meta
	PremisesID    int64   `json:"premises_id" db:"premises_id"`
	PersonID      *int64  `json:"person_id" db:"person_id"`
	LegalEntityID *int64  `json:"legal_entity_id" db:"legal_entity_id"`
	ShareNum      int     `json:"share_num" db:"share_num"`
	ShareDen      int     `json:"share_den" db:"share_den"`
	ValidFrom     Date    `json:"valid_from" db:"valid_from"`
	ValidTo       *Date   `json:"valid_to" db:"valid_to"`
	Basis         *string `json:"basis" db:"basis"`
}

func (o Ownership) Validate() error {
	var share error
	switch {
	case o.ShareNum <= 0 || o.ShareDen <= 0:
		share = invalid("share_num", "share_num and share_den must be positive")
	case o.ShareNum > o.ShareDen:
		share = invalid("share_num", "share must not exceed 1")
	}
	return firstErr(
		positiveInt("premises_id", intPtr(o.PremisesID)),
		exactlyOne(o.PersonID, o.LegalEntityID),
		share,
		period(o.ValidFrom, o.ValidTo),
	)
}

// OwnershipView — владение вместе с названием владельца (для вложенных ответов).
type OwnershipView struct {
	Ownership
	OwnerKind string `json:"owner_kind" db:"owner_kind"`
	OwnerName string `json:"owner_name" db:"owner_name"`
}

// SetDefaults: если доля не указана, это вся собственность (1/1).
func (o *Ownership) SetDefaults() {
	if o.ShareNum == 0 && o.ShareDen == 0 {
		o.ShareNum, o.ShareDen = 1, 1
	}
}

// OwnedPremises — помещение, которым сейчас владеет физлицо или юрлицо.
type OwnedPremises struct {
	OwnershipID     int64    `json:"ownership_id" db:"ownership_id"`
	PremisesID      int64    `json:"premises_id" db:"premises_id"`
	PremisesKind    string   `json:"premises_kind" db:"premises_kind"`
	PremisesNumber  string   `json:"premises_number" db:"premises_number"`
	TotalArea       *float64 `json:"total_area" db:"total_area"`
	BuildingID      int64    `json:"building_id" db:"building_id"`
	BuildingAddress string   `json:"building_address" db:"building_address"`
	ShareNum        int      `json:"share_num" db:"share_num"`
	ShareDen        int      `json:"share_den" db:"share_den"`
	ValidFrom       Date     `json:"valid_from" db:"valid_from"`
	ValidTo         *Date    `json:"valid_to" db:"valid_to"`
}
