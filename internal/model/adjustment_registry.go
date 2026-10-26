package model

import (
	"math"
	"strings"
	"time"
)

// AdjustmentRegistry — корректировочный реестр: файл Сбер-формата с одним условным платежом на лицевой счёт.
// Сам файл хранится в БД и отдаётся отдельным запросом. Поля с account_/premises_/building_ — из связанных записей (только чтение).
type AdjustmentRegistry struct {
	Meta
	BankAccountID     int64      `json:"bank_account_id" db:"bank_account_id"`
	PersonalAccountID int64      `json:"personal_account_id" db:"personal_account_id"`
	Number            int        `json:"number" db:"number"`
	Amount            float64    `json:"amount" db:"amount"`
	PayerName         string     `json:"payer_name" db:"payer_name"`
	Comment           *string    `json:"comment" db:"comment"`
	FileName          string     `json:"file_name" db:"file_name"`
	ResolvedAt        *time.Time `json:"resolved_at" db:"resolved_at"`
	AccountNumber     string     `json:"account_number" db:"account_number"`
	PremisesID        int64      `json:"premises_id" db:"premises_id"`
	PremisesKind      string     `json:"premises_kind" db:"premises_kind"`
	PremisesNumber    string     `json:"premises_number" db:"premises_number"`
	BuildingName      *string    `json:"building_name" db:"building_name"`
	BuildingAddress   string     `json:"building_address" db:"building_address"`
}

// AdjustmentRegistryRequest — тело запроса на создание реестра.
type AdjustmentRegistryRequest struct {
	BankAccountID     int64   `json:"bank_account_id"`
	PersonalAccountID int64   `json:"personal_account_id"`
	Amount            float64 `json:"amount"`
	PayerName         string  `json:"payer_name"`
	Comment           *string `json:"comment"`
}

// MaxAdjustmentAmount — предел суммы одного корректировочного реестра, ₽: защита от опечатки.
const MaxAdjustmentAmount = 10_000_000

func (r *AdjustmentRegistryRequest) Validate() error {
	r.PayerName = strings.TrimSpace(r.PayerName)
	var amount error
	switch {
	case r.Amount <= 0:
		amount = invalid("amount", "must be positive")
	case r.Amount > MaxAdjustmentAmount:
		amount = invalid("amount", "is too large")
	case math.Abs(r.Amount*100-math.Round(r.Amount*100)) > 1e-6:
		amount = invalid("amount", "must have at most 2 decimal places")
	}
	return firstErr(
		positiveInt("bank_account_id", intPtr(r.BankAccountID)),
		positiveInt("personal_account_id", intPtr(r.PersonalAccountID)),
		amount,
		required("payer_name", r.PayerName),
	)
}

type AdjustmentRegistryFilter struct {
	Deleted           bool
	BankAccountID     *int64
	PersonalAccountID *int64
	// Resolved: true — только снятые, false — только не снятые, nil — все.
	Resolved *bool
}
