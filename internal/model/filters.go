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
	Q          *string // начало номера помещения (для поиска при вводе)
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
	Q          *string // начало номера лицевого счёта (для поиска при вводе)
	Status     *string
	Purpose    *string
}

type AccountHolderFilter struct {
	Deleted       bool // true — только удалённые (корзина), false — только действующие
	AccountID     *int64
	PersonID      *int64
	LegalEntityID *int64
}

type BankAccountFilter struct {
	Deleted        bool // true — только удалённые (корзина), false — только действующие
	OrganizationID *int64
	IsSpecial      *bool
	Active         *bool // вычисляется по периоду действия на сегодня
}

type PaymentCategoryFilter struct {
	Deleted   bool
	Direction *string
}

type PaymentRegistryFilter struct {
	Deleted       bool
	BankAccountID *int64
	DateFrom      *Date // по дате реестра
	DateTo        *Date
	Q             *string // подстрока в имени файла или номере реестра
}

type IncomingPaymentFilter struct {
	Deleted           bool
	BankAccountID     *int64
	RegistryID        *int64
	StatementID       *int64
	PersonalAccountID *int64
	CategoryID        *int64
	DateFrom          *Date
	DateTo            *Date
	AmountFrom        *float64
	AmountTo          *float64
	Q                 *string // подстрока в плательщике, назначении, комментарии, номере документа
	Unlinked          bool    // только без лицевого счёта и категории
}

type OutgoingPaymentFilter struct {
	Deleted       bool
	BankAccountID *int64
	StatementID   *int64
	CategoryID    *int64
	DateFrom      *Date
	DateTo        *Date
	AmountFrom    *float64
	AmountTo      *float64
	Q             *string // подстрока в получателе, назначении, комментарии, номере документа
}

// AssignmentScope — какие входящие платежи участвуют в определении лицевых счетов (те же фильтры, что у списка платежей).
type AssignmentScope struct {
	BankAccountID *int64   `json:"bank_account_id,omitempty"`
	RegistryID    *int64   `json:"registry_id,omitempty"`
	StatementID   *int64   `json:"statement_id,omitempty"`
	CategoryID    *int64   `json:"category_id,omitempty"`
	DateFrom      *Date    `json:"date_from,omitempty"`
	DateTo        *Date    `json:"date_to,omitempty"`
	AmountFrom    *float64 `json:"amount_from,omitempty"`
	AmountTo      *float64 `json:"amount_to,omitempty"`
	Q             *string  `json:"q,omitempty"`
}
