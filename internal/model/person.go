package model

// Person — физлицо: собственник, житель или плательщик.
type Person struct {
	Meta
	LastName   string  `json:"last_name" db:"last_name"`
	FirstName  string  `json:"first_name" db:"first_name"`
	MiddleName *string `json:"middle_name" db:"middle_name"`
	BirthDate  *Date   `json:"birth_date" db:"birth_date"`
	Phone      *string `json:"phone" db:"phone"`
	Email      *string `json:"email" db:"email"`
}

func (p Person) Validate() error {
	return firstErr(required("last_name", p.LastName), required("first_name", p.FirstName))
}

// LegalEntity — юрлицо-собственник (например, коммерческих помещений).
type LegalEntity struct {
	Meta
	Name string  `json:"name" db:"name"`
	INN  string  `json:"inn" db:"inn"`
	KPP  *string `json:"kpp" db:"kpp"`
	OGRN *string `json:"ogrn" db:"ogrn"`
}

func (l LegalEntity) Validate() error {
	return firstErr(required("name", l.Name), required("inn", l.INN))
}
