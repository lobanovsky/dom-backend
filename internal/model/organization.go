package model

var OrganizationKinds = []string{"uk", "tsn", "tszh"}

// Organization — управляющая организация: УК или ТСН/ТСЖ.
type Organization struct {
	Meta
	Kind string  `json:"kind" db:"kind"`
	Name string  `json:"name" db:"name"`
	INN  *string `json:"inn" db:"inn"`
	KPP  *string `json:"kpp" db:"kpp"`
	OGRN *string `json:"ogrn" db:"ogrn"`
}

func (o Organization) Validate() error {
	return firstErr(
		oneOf("kind", o.Kind, OrganizationKinds),
		required("name", o.Name),
	)
}
