package model

import "time"

type DebtorReport struct {
	Period     Date      `json:"period" db:"period"`
	FileName   string    `json:"file_name" db:"file_name"`
	ImportedAt time.Time `json:"imported_at" db:"imported_at"`
	RowsCount  int       `json:"rows_count" db:"rows_count"`
}

type DebtorReportRow struct {
	AccountNumber string   `json:"account_number" db:"account_number"`
	Debt          float64  `json:"debt" db:"debt"`
	AccountID     *int64   `json:"account_id" db:"account_id"`
	Purpose       *string  `json:"purpose" db:"purpose"`
	PremisesID    *int64   `json:"premises_id" db:"premises_id"`
	PremisesKind  *string  `json:"premises_kind" db:"premises_kind"`
	PremisesNo    *string  `json:"premises_number" db:"premises_number"`
	HolderNames   []string `json:"holder_names" db:"holder_names"`
	Phones        []string `json:"phones" db:"phones"`
	Emails        []string `json:"emails" db:"emails"`
}

type DebtorImportRow struct {
	AccountNumber string
	Debt          float64
}

type DebtorImportResult struct {
	Period    Date `json:"period"`
	RowsCount int  `json:"rows_count"`
}
