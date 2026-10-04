package model

var ResidencyRelations = []string{"spouse", "child", "parent", "relative", "tenant", "other"}

// Residency — проживание человека в помещении. Родство с собственником необязательно.
type Residency struct {
	Meta
	PersonID       int64  `json:"person_id" db:"person_id"`
	PremisesID     int64  `json:"premises_id" db:"premises_id"`
	Registered     bool   `json:"registered" db:"registered"`
	Relation       string `json:"relation" db:"relation"`
	RelatedOwnerID *int64 `json:"related_owner_id" db:"related_owner_id"`
	ValidFrom      Date   `json:"valid_from" db:"valid_from"`
	ValidTo        *Date  `json:"valid_to" db:"valid_to"`
}

func (r Residency) Validate() error {
	return firstErr(
		positiveInt("person_id", intPtr(r.PersonID)),
		positiveInt("premises_id", intPtr(r.PremisesID)),
		oneOf("relation", r.Relation, ResidencyRelations),
		period(r.ValidFrom, r.ValidTo),
	)
}

// SetDefaults подставляет значения по умолчанию для не переданных полей.
func (r *Residency) SetDefaults() {
	if r.Relation == "" {
		r.Relation = "other"
	}
}
