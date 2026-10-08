package model

import "time"

// BankStatement — загруженная банковская выписка (xlsx). Сам файл хранится в БД, в ответы API не входит.
// Остатки со знаком: кредитовый остаток счёта положительный.
type BankStatement struct {
	Meta
	BankAccountID  int64    `json:"bank_account_id" db:"bank_account_id"`
	FileName       string   `json:"file_name" db:"file_name"`
	FileSHA256     string   `json:"file_sha256" db:"file_sha256"`
	PeriodFrom     *Date    `json:"period_from" db:"period_from"`
	PeriodTo       *Date    `json:"period_to" db:"period_to"`
	OpeningBalance *float64 `json:"opening_balance" db:"opening_balance"`
	ClosingBalance *float64 `json:"closing_balance" db:"closing_balance"`
	DebitCount     int      `json:"debit_count" db:"debit_count"`
	CreditCount    int      `json:"credit_count" db:"credit_count"`
	DebitTotal     float64  `json:"debit_total" db:"debit_total"`
	CreditTotal    float64  `json:"credit_total" db:"credit_total"`
}

type BankStatementFilter struct {
	Deleted       bool
	BankAccountID *int64
	DateFrom      *Date // по концу периода выписки
	DateTo        *Date // по началу периода выписки
	Q             *string
}

// StatementOperation — строка выписки. Суммы в копейках. Контрагент — противоположная сторона операции:
// у поступления это плательщик, у списания получатель.
type StatementOperation struct {
	Row            int // номер строки в файле (с 1)
	At             time.Time
	Outgoing       bool // списание (сумма по дебету)
	Amount         int64
	CounterAccount string
	CounterINN     string
	CounterName    string
	DocNumber      string
	OperationType  string // ВО
	BIK            string
	BankName       string
	Purpose        string
	Raw            string // строка целиком, для аудита
	DedupKey       string // опознаёт одну и ту же операцию в пересекающихся выписках
}

// ParsedStatement — разобранный файл выписки. Суммы в копейках.
type ParsedStatement struct {
	Sheet          string // лист файла, из которого разобрана выписка
	MultiSheet     bool   // в файле несколько выписок (по листу на счёт)
	Account        string // наш счёт
	PeriodFrom     *Date
	PeriodTo       *Date
	OpeningBalance *int64
	ClosingBalance *int64
	DebitCount     int
	CreditCount    int
	DebitTotal     int64
	CreditTotal    int64
	Operations     []StatementOperation
}

// SkippedOperation — операция, которая не загружена, потому что уже есть в базе (из другой выписки).
type SkippedOperation struct {
	PaymentDate Date    `json:"payment_date"`
	Outgoing    bool    `json:"outgoing"`
	Counterpart string  `json:"counterpart"`
	DocNumber   string  `json:"doc_number"`
	Amount      float64 `json:"amount"`
}

// StatementImportResult — итог загрузки одной выписки.
type StatementImportResult struct {
	StatementID       int64              `json:"statement_id"`
	Incoming          int                `json:"incoming"`
	Outgoing          int                `json:"outgoing"`
	SkippedDuplicates int                `json:"skipped_duplicates"`
	Skipped           []SkippedOperation `json:"skipped"`
	Warnings          []string           `json:"warnings"`
}
