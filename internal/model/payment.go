package model

import (
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

var PaymentDirections = []string{"incoming", "outgoing"}

// PaymentCategory — справочник категорий: для входящих платежей без лицевого счёта
// (аренда оборудования и т.п.) и для классификации расходов.
type PaymentCategory struct {
	Meta
	Name      string  `json:"name" db:"name"`
	Direction string  `json:"direction" db:"direction"`
	Color     *string `json:"color" db:"color"` // #rrggbb, для оформления в таблицах
	Icon      *string `json:"icon" db:"icon"`   // значок (эмодзи), для оформления в таблицах
}

var categoryColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (c PaymentCategory) Validate() error {
	err := firstErr(required("name", c.Name), oneOf("direction", c.Direction, PaymentDirections))
	if err != nil {
		return err
	}
	if c.Color != nil && !categoryColorRe.MatchString(*c.Color) {
		return invalid("color", "must be #rrggbb")
	}
	if c.Icon != nil && (strings.TrimSpace(*c.Icon) == "" || utf8.RuneCountInString(*c.Icon) > 16) {
		return invalid("icon", "must be 1 to 16 characters")
	}
	return nil
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
	Attention         bool     `json:"attention" db:"attention"` // «обратить внимание»: при включённом флаге комментарий обязателен
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
	// Только чтение: название правила, номер лицевого счёта и номер и имя файла реестра для отображения в списках.
	RuleName              *string `json:"rule_name" db:"rule_name"`
	PersonalAccountNumber *string `json:"personal_account_number" db:"personal_account_number"`
	PremisesID            *int64  `json:"premises_id" db:"premises_id"` // помещение лицевого счёта: по нему в списке строится ссылка
	RegistryNumber        *string `json:"registry_number" db:"registry_number"`
	RegistryFileName      *string `json:"registry_file_name" db:"registry_file_name"` // имя файла реестра: в списках говорит больше, чем номер
	// Только чтение: если этот платёж из банка детализирован реестром (в назначении имя файла реестра) — сам реестр.
	DetailRegistryID       *int64  `json:"detail_registry_id" db:"detail_registry_id"`
	DetailRegistryFileName *string `json:"detail_registry_file_name" db:"detail_registry_file_name"`
	DetailCount            *int    `json:"detail_count" db:"detail_count"`
	// Только чтение: выгрузка в реестр (payment_exports), в которую вошёл платёж; nil — ещё не выгружен.
	ExportID       *int64  `json:"export_id" db:"export_id"`
	ExportFileName *string `json:"export_file_name" db:"export_file_name"`
}

func (p IncomingPayment) Validate() error {
	var link error
	if p.PersonalAccountID != nil && p.CategoryID != nil {
		link = invalid("category_id", "must not be set together with personal_account_id")
	}
	var attention error
	if p.Attention && (p.Comment == nil || strings.TrimSpace(*p.Comment) == "") {
		attention = invalid("comment", "is required when attention is set")
	}
	return firstErr(
		positiveInt("bank_account_id", intPtr(p.BankAccountID)),
		requiredDate("payment_date", p.PaymentDate),
		positiveAmount("amount", p.Amount),
		optionalINN("payer_inn", p.PayerINN),
		optionalDigits("payer_bik", p.PayerBIK, 9),
		link,
		attention,
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
	// Платёж из выписки, который этот реестр детализирует (сопоставляется сервером), и его данные для отображения.
	PaymentID          *int64   `json:"payment_id" db:"payment_id"`
	PaymentDate        *Date    `json:"payment_date" db:"payment_date"`
	PaymentAmount      *float64 `json:"payment_amount" db:"payment_amount"`
	PaymentStatementID *int64   `json:"payment_statement_id" db:"payment_statement_id"`
}

// PaymentExport — выгрузка входящих платежей в реестр Сбер-формата. Сам файл хранится в БД и отдаётся отдельным запросом,
// в ответы API его содержимое не входит.
type PaymentExport struct {
	Meta
	BankAccountID int64   `json:"bank_account_id" db:"bank_account_id"`
	Number        int     `json:"number" db:"number"`
	FileName      string  `json:"file_name" db:"file_name"`
	PaymentsCount int     `json:"payments_count" db:"payments_count"`
	TotalAmount   float64 `json:"total_amount" db:"total_amount"`
}
