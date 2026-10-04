package model

// Фильтры списков: nil — фильтр не задан.

type OrganizationFilter struct {
	Kind *string
}

type BuildingFilter struct {
	OrganizationID *int64
	Kind           *string
}

type PremisesFilter struct {
	BuildingID *int64
	Kind       *string
	Number     *string
}

type PersonFilter struct {
	LastName *string
	Phone    *string
	Q        *string // подстрока в ФИО или телефоне
}

type LegalEntityFilter struct {
	INN *string
	Q   *string // подстрока в названии или ИНН
}

type OwnershipFilter struct {
	PremisesID    *int64
	PersonID      *int64
	LegalEntityID *int64
}

type ResidencyFilter struct {
	PremisesID     *int64
	PersonID       *int64
	RelatedOwnerID *int64
}

type AccountFilter struct {
	PremisesID *int64
	Number     *string
	Status     *string
	Purpose    *string
}

type AccountHolderFilter struct {
	AccountID     *int64
	PersonID      *int64
	LegalEntityID *int64
}
