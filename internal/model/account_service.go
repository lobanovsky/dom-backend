package model

// AccountService — назначение услуги лицевому счёту на период [ValidFrom, ValidTo] (включительно; пустой ValidTo — продолжается).
// Quantity задаётся только у услуг с ручным вводом количества (calc_method = manual), например «изготовление ключей, 2 шт.».
// Разовая услуга — назначение на один месяц. Соответствие услуги и счёта (тип счёта, вид помещения) проверяет БД.
// Поля счёта, помещения и услуги вычисляются сервером и при записи игнорируются.
type AccountService struct {
	Meta
	AccountID int64    `json:"account_id" db:"account_id"`
	ServiceID int64    `json:"service_id" db:"service_id"`
	ValidFrom Date     `json:"valid_from" db:"valid_from"`
	ValidTo   *Date    `json:"valid_to" db:"valid_to"`
	Quantity  *float64 `json:"quantity" db:"quantity"`
	Comment   *string  `json:"comment" db:"comment"`

	AccountNumber   string   `json:"account_number" db:"account_number"`
	PremisesID      int64    `json:"premises_id" db:"premises_id"`
	PremisesKind    string   `json:"premises_kind" db:"premises_kind"`
	PremisesNumber  string   `json:"premises_number" db:"premises_number"`
	BuildingAddress string   `json:"building_address" db:"building_address"`
	ServiceName     string   `json:"service_name" db:"service_name"`
	ServiceUnit     string   `json:"service_unit" db:"service_unit"`
	CurrentPrice    *float64 `json:"current_price" db:"current_price"`
}

func (a AccountService) Validate() error {
	var period, quantity error
	if a.ValidTo != nil && a.ValidTo.Before(a.ValidFrom.Time) {
		period = invalid("valid_to", "must not be before valid_from")
	}
	if a.Quantity != nil {
		if *a.Quantity <= 0 {
			quantity = invalid("quantity", "must be positive")
		} else {
			quantity = decimals("quantity", *a.Quantity, 3)
		}
	}
	return firstErr(
		positiveInt("account_id", intPtr(a.AccountID)),
		positiveInt("service_id", intPtr(a.ServiceID)),
		requiredDate("valid_from", a.ValidFrom),
		period,
		quantity,
	)
}

// AccountServiceBulkRequest — массовое назначение услуги счетам. Кандидаты: действующие счета того типа, что у услуги,
// в помещениях подходящих видов (PremisesKinds сужает виды услуги; пусто — все виды услуги) и дома BuildingID (пусто — любого).
// Счета, у которых услуга уже назначена на пересекающийся период, пропускаются.
type AccountServiceBulkRequest struct {
	ServiceID     int64    `json:"service_id"`
	ValidFrom     Date     `json:"valid_from"`
	ValidTo       *Date    `json:"valid_to"`
	Quantity      *float64 `json:"quantity"`
	BuildingID    *int64   `json:"building_id"`
	PremisesKinds []string `json:"premises_kinds"`
}

func (r AccountServiceBulkRequest) Validate() error {
	var kinds error
	for _, k := range r.PremisesKinds {
		if err := oneOf("premises_kinds", k, PremisesKinds); err != nil {
			kinds = err
			break
		}
	}
	return firstErr(
		AccountService{AccountID: 1, ServiceID: r.ServiceID, ValidFrom: r.ValidFrom, ValidTo: r.ValidTo, Quantity: r.Quantity}.Validate(),
		kinds,
	)
}

// AccountServiceBulkSample — счёт из предпросмотра массового назначения.
type AccountServiceBulkSample struct {
	AccountID       int64  `json:"account_id"`
	AccountNumber   string `json:"account_number"`
	PremisesKind    string `json:"premises_kind"`
	PremisesNumber  string `json:"premises_number"`
	BuildingAddress string `json:"building_address"`
}

// AccountServiceBulkPreview — что сделает массовое назначение.
type AccountServiceBulkPreview struct {
	Candidates      int                        `json:"candidates"`       // подходящих счетов
	ToAssign        int                        `json:"to_assign"`        // получат услугу
	SkippedExisting int                        `json:"skipped_existing"` // уже есть назначение на пересекающийся период
	Sample          []AccountServiceBulkSample `json:"sample"`           // первые из тех, кто получит услугу
}

// AccountServiceBulkResult — итог массового назначения.
type AccountServiceBulkResult struct {
	Created int `json:"created"`
}

// AccountServiceCloseRequest — закрыть датой открытые назначения услуги.
type AccountServiceCloseRequest struct {
	ServiceID  int64  `json:"service_id"`
	ValidTo    Date   `json:"valid_to"`
	BuildingID *int64 `json:"building_id"`
}

func (r AccountServiceCloseRequest) Validate() error {
	return firstErr(positiveInt("service_id", intPtr(r.ServiceID)), requiredDate("valid_to", r.ValidTo))
}

// AccountServiceCloseResult — сколько назначений закрыто.
type AccountServiceCloseResult struct {
	Closed int `json:"closed"`
}
