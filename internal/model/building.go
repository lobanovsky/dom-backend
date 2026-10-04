package model

var BuildingKinds = []string{"apartment_building", "parking", "common_premises", "other"}

// Building — дом, паркинг или помещения общего пользования.
type Building struct {
	Meta
	OrganizationID  int64    `json:"organization_id" db:"organization_id"`
	Kind            string   `json:"kind" db:"kind"`
	Address         string   `json:"address" db:"address"`
	CadastralNumber *string  `json:"cadastral_number" db:"cadastral_number"`
	Floors          *int     `json:"floors" db:"floors"`
	Entrances       *int     `json:"entrances" db:"entrances"`
	TotalArea       *float64 `json:"total_area" db:"total_area"`
	YearBuilt       *int     `json:"year_built" db:"year_built"`
}

func (b Building) Validate() error {
	var year error
	if b.YearBuilt != nil && (*b.YearBuilt < 1700 || *b.YearBuilt > 2200) {
		year = invalid("year_built", "must be between 1700 and 2200")
	}
	return firstErr(
		positiveInt("organization_id", intPtr(b.OrganizationID)),
		oneOf("kind", b.Kind, BuildingKinds),
		required("address", b.Address),
		positiveInt("floors", b.Floors),
		positiveInt("entrances", b.Entrances),
		positive("total_area", b.TotalArea),
		year,
	)
}

func intPtr(v int64) *int { i := int(v); return &i }
