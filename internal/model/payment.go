package model

import "math"

var PaymentDirections = []string{"incoming", "outgoing"}

// PaymentCategory — справочник категорий: для входящих платежей без лицевого счёта
// (аренда оборудования и т.п.) и для классификации расходов.
type PaymentCategory struct {
	Meta
	Name      string `json:"name" db:"name"`
	Direction string `json:"direction" db:"direction"`
}

func (c PaymentCategory) Validate() error {
	return firstErr(required("name", c.Name), oneOf("direction", c.Direction, PaymentDirections))
}

// IncomingPayment — поступление на наш банковский счёт. Платёж привязывается не более чем к одному
// лицевому счёту; платёж без лицевого счёта может иметь категорию. registry_id и external_id
// заполняются при загрузке реестра, raw_line хранит исходную строку реестра.
type IncomingPayment struct {
	Meta
	BankAccountID     int64    `json:"bank_account_id" db:"bank_account_id"`
	RegistryID        *int64   `json:"registry_id" db:"registry_id"`
	ExternalID        *string  `json:"external_id" db:"external_id"`
	PaymentDate       Date     `json:"payment_date" db:"payment_date"`
	PaymentTime       *Clock   `json:"payment_time" db:"payment_time"`
	Amount            float64  `json:"amount" db:"amount"`
	Commission        *float64 `json:"commission" db:"commission"`
	PayerName         string   `json:"payer_name" db:"payer_name"` // может быть пустым: в реестрах ФИО не всегда указано
	PayerINN          *string  `json:"payer_inn" db:"payer_inn"`
	PayerAccount      *string  `json:"payer_account" db:"payer_account"`
	PayerBIK          *string  `json:"payer_bik" db:"payer_bik"`
	PayerBankName     *string  `json:"payer_bank_name" db:"payer_bank_name"`
	DocNumber         *string  `json:"doc_number" db:"doc_number"`
	OperationType     *string  `json:"operation_type" db:"operation_type"`
	Purpose           *string  `json:"purpose" db:"purpose"`
	Comment           *string  `json:"comment" db:"comment"`
	PersonalAccountID *int64   `json:"personal_account_id" db:"personal_account_id"`
	CategoryID        *int64   `json:"category_id" db:"category_id"`
	RawLine           *string  `json:"raw_line" db:"raw_line"`
	// Происхождение: выписка и ключ опознания операции (служебный, наружу не отдаётся).
	StatementID *int64  `json:"statement_id" db:"statement_id"`
	DedupKey    *string `json:"-" db:"dedup_key"`
	// Происхождение привязки к лицевому счёту/категории: registry | manual | rule (выставляет сервер).
	AssignedBy *string `json:"assigned_by" db:"assigned_by"`
	RuleID     *int64  `json:"rule_id" db:"rule_id"`
	RunID      *int64  `json:"run_id" db:"run_id"`
	// Только чтение: название правила, номер лицевого счёта и номер реестра для отображения в списках.
	RuleName              *string `json:"rule_name" db:"rule_name"`
	PersonalAccountNumber *string `json:"personal_account_number" db:"personal_account_number"`
	PremisesID            *int64  `json:"premises_id" db:"premises_id"` // помещение лицевого счёта: по нему в списке строится ссылка
	RegistryNumber        *string `json:"registry_number" db:"registry_number"`
}

func (p IncomingPayment) Validate() error {
	var link error
	if p.PersonalAccountID != nil && p.CategoryID != nil {
		link = invalid("category_id", "must not be set together with personal_account_id")
	}
	return firstErr(
		positiveInt("bank_account_id", intPtr(p.BankAccountID)),
		requiredDate("payment_date", p.PaymentDate),
		positiveAmount("amount", p.Amount),
		optionalINN("payer_inn", p.PayerINN),
		optionalDigits("payer_bik", p.PayerBIK, 9),
		link,
	)
}

// OutgoingPayment — списание с нашего банковского счёта.
type OutgoingPayment struct {
	Meta
	BankAccountID     int64   `json:"bank_account_id" db:"bank_account_id"`
	PaymentDate       Date    `json:"payment_date" db:"payment_date"`
	Amount            float64 `json:"amount" db:"amount"`
	RecipientName     string  `json:"recipient_name" db:"recipient_name"`
	RecipientINN      *string `json:"recipient_inn" db:"recipient_inn"`
	RecipientAccount  *string `json:"recipient_account" db:"recipient_account"`
	RecipientBIK      *string `json:"recipient_bik" db:"recipient_bik"`
	RecipientBankName *string `json:"recipient_bank_name" db:"recipient_bank_name"`
	DocNumber         *string `json:"doc_number" db:"doc_number"`
	OperationType     *string `json:"operation_type" db:"operation_type"`
	Purpose           *string `json:"purpose" db:"purpose"`
	Comment           *string `json:"comment" db:"comment"`
	CategoryID        *int64  `json:"category_id" db:"category_id"`
	StatementID       *int64  `json:"statement_id" db:"statement_id"`
	DedupKey          *string `json:"-" db:"dedup_key"`
	// Происхождение категории: manual | rule (выставляет сервер); название правила — только для чтения.
	AssignedBy *string `json:"assigned_by" db:"assigned_by"`
	RuleID     *int64  `json:"rule_id" db:"rule_id"`
	RunID      *int64  `json:"run_id" db:"run_id"`
	RuleName   *string `json:"rule_name" db:"rule_name"`
}

func (p OutgoingPayment) Validate() error {
	return firstErr(
		positiveInt("bank_account_id", intPtr(p.BankAccountID)),
		requiredDate("payment_date", p.PaymentDate),
		positiveAmount("amount", p.Amount),
		required("recipient_name", p.RecipientName),
		optionalINN("recipient_inn", p.RecipientINN),
		optionalDigits("recipient_bik", p.RecipientBIK, 9),
	)
}

func requiredDate(field string, d Date) error {
	if d.IsZero() {
		return invalid(field, "is required")
	}
	return nil
}

// positiveAmount: сумма платежа больше нуля и не более двух знаков после запятой.
func positiveAmount(field string, v float64) error {
	if v <= 0 {
		return invalid(field, "must be positive")
	}
	if math.Abs(v*100-math.Round(v*100)) > 1e-6 {
		return invalid(field, "must have at most 2 decimal places")
	}
	return nil
}

// PaymentRegistry — загруженный файл реестра платежей. Сам файл хранится в БД (скачивание и перечитывание),
// в ответы API его содержимое не входит.
type PaymentRegistry struct {
	Meta
	BankAccountID    int64   `json:"bank_account_id" db:"bank_account_id"`
	Source           string  `json:"source" db:"source"`
	FileName         string  `json:"file_name" db:"file_name"`
	FileSHA256       string  `json:"file_sha256" db:"file_sha256"`
	RegistryNumber   *string `json:"registry_number" db:"registry_number"`
	RegistryDate     *Date   `json:"registry_date" db:"registry_date"`
	PaymentsCount    int     `json:"payments_count" db:"payments_count"`
	TotalAmount      float64 `json:"total_amount" db:"total_amount"`
	TotalTransferred float64 `json:"total_transferred" db:"total_transferred"`
	TotalCommission  float64 `json:"total_commission" db:"total_commission"`
}
