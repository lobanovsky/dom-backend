package model

var (
	AccountPurposes = []string{"utilities", "capital_repair", "parking", "other"}
	AccountStatuses = []string{"active", "closed"}
)

// Account — лицевой счёт помещения.
type Account struct {
	Meta
	Number     string `json:"number" db:"number"`
	PremisesID int64  `json:"premises_id" db:"premises_id"`
	Purpose    string `json:"purpose" db:"purpose"`
	Status     string `json:"status" db:"status"`
	OpenedAt   Date   `json:"opened_at" db:"opened_at"`
	ClosedAt   *Date  `json:"closed_at" db:"closed_at"`
}

func (a Account) Validate() error {
	var closed error
	if a.OpenedAt.IsZero() {
		closed = invalid("opened_at", "is required")
	} else if a.ClosedAt != nil && a.ClosedAt.Before(a.OpenedAt.Time) {
		closed = invalid("closed_at", "must not be before opened_at")
	}
	return firstErr(
		required("number", a.Number),
		positiveInt("premises_id", intPtr(a.PremisesID)),
		oneOf("purpose", a.Purpose, AccountPurposes),
		oneOf("status", a.Status, AccountStatuses),
		closed,
	)
}

// AccountView — счёт с названиями всех текущих плательщиков (по порядку начала периода).
type AccountView struct {
	Account
	HolderNames []string `json:"holder_names" db:"holder_names"`
}

// AccountHolder — плательщик по лицевому счёту в заданный период.
type AccountHolder struct {
	Meta
	AccountID     int64  `json:"account_id" db:"account_id"`
	PersonID      *int64 `json:"person_id" db:"person_id"`
	LegalEntityID *int64 `json:"legal_entity_id" db:"legal_entity_id"`
	ValidFrom     Date   `json:"valid_from" db:"valid_from"`
	ValidTo       *Date  `json:"valid_to" db:"valid_to"`
}

func (h AccountHolder) Validate() error {
	return firstErr(
		positiveInt("account_id", intPtr(h.AccountID)),
		exactlyOne(h.PersonID, h.LegalEntityID),
		period(h.ValidFrom, h.ValidTo),
	)
}

// SetDefaults подставляет значения по умолчанию для не переданных полей.
func (a *Account) SetDefaults() {
	if a.Purpose == "" {
		a.Purpose = "utilities"
	}
	if a.Status == "" {
		a.Status = "active"
	}
}
