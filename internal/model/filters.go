package model

// Фильтры списков: nil — фильтр не задан.

type OrganizationFilter struct {
	Deleted bool // true — только удалённые (корзина), false — только действующие
	Kind    *string
}

type BuildingFilter struct {
	Deleted        bool // true — только удалённые (корзина), false — только действующие
	OrganizationID *int64
	Kind           *string
}

type PremisesFilter struct {
	Deleted    bool // true — только удалённые (корзина), false — только действующие
	BuildingID *int64
	Kind       *string
	Number     *string
}

type PersonFilter struct {
	Deleted  bool // true — только удалённые (корзина), false — только действующие
	LastName *string
	Phone    *string
	Q        *string // подстрока в ФИО или телефоне
}

type LegalEntityFilter struct {
	Deleted bool // true — только удалённые (корзина), false — только действующие
	INN     *string
	Q       *string // подстрока в названии или ИНН
}

type OwnershipFilter struct {
	Deleted       bool // true — только удалённые (корзина), false — только действующие
	PremisesID    *int64
	PersonID      *int64
	LegalEntityID *int64
}

type ResidencyFilter struct {
	Deleted        bool // true — только удалённые (корзина), false — только действующие
	PremisesID     *int64
	PersonID       *int64
	RelatedOwnerID *int64
}

type AccountFilter struct {
	Deleted    bool // true — только удалённые (корзина), false — только действующие
	PremisesID *int64
	Number     *string
	Status     *string
	Purpose    *string
}

type AccountHolderFilter struct {
	Deleted       bool // true — только удалённые (корзина), false — только действующие
	AccountID     *int64
	PersonID      *int64
	LegalEntityID *int64
}
