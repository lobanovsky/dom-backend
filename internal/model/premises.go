package model

var PremisesKinds = []string{"apartment", "non_residential", "commercial", "parking_space", "storage"}

// Premises — объект недвижимости в доме. Лицевые счета и владение привязаны к нему.
type Premises struct {
	Meta
	BuildingID      int64    `json:"building_id" db:"building_id"`
	Kind            string   `json:"kind" db:"kind"`
	Number          string   `json:"number" db:"number"`
	Entrance        *int     `json:"entrance" db:"entrance"`
	Floor           *int     `json:"floor" db:"floor"`
	TotalArea       *float64 `json:"total_area" db:"total_area"`
	LivingArea      *float64 `json:"living_area" db:"living_area"`
	Rooms           *int     `json:"rooms" db:"rooms"`
	CadastralNumber *string  `json:"cadastral_number" db:"cadastral_number"`
}

func (p Premises) Validate() error {
	var area error
	if p.LivingArea != nil && p.TotalArea != nil && *p.LivingArea > *p.TotalArea {
		area = invalid("living_area", "must not exceed total_area")
	}
	return firstErr(
		positiveInt("building_id", intPtr(p.BuildingID)),
		oneOf("kind", p.Kind, PremisesKinds),
		required("number", p.Number),
		positive("total_area", p.TotalArea),
		positive("living_area", p.LivingArea),
		positiveInt("rooms", p.Rooms),
		area,
	)
}
