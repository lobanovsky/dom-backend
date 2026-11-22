package model

import "time"

// ImportRow — одна строка xlsx-файла импорта: помещение, его собственник и два лицевых счёта.
type ImportRow struct {
	Row                  int // номер строки в файле (с 1)
	Number               string
	Area                 float64
	CadastralNumber      string
	LastName             string
	FirstName            string
	MiddleName           string
	UtilitiesAccount     string // лицевой счёт ЖКУ
	CapitalRepairAccount string // лицевой счёт капитального ремонта
}

// ImportRowError — ошибка в конкретной строке файла.
type ImportRowError struct {
	Row   int    `json:"row"`
	Error string `json:"error"`
}

// ImportResult — что создано при импорте.
type ImportResult struct {
	Premises       int `json:"premises"`
	Accounts       int `json:"accounts"`
	Ownerships     int `json:"ownerships"`
	PersonsCreated int `json:"persons_created"`
	PersonsReused  int `json:"persons_reused"`
}

// MeterReadingImportRow — одна строка xlsx-файла с показаниями: помещение, номер счётчика и показания по тарифам.
type MeterReadingImportRow struct {
	Row      int // номер строки в файле (с 1)
	Premises string
	Serial   string
	T        [3]*float64 // заполненные подряд тарифы с первого; число заполненных — число тарифов счётчика
}

// Tariffs — число заполненных показаний в строке.
func (r MeterReadingImportRow) Tariffs() int {
	n := 0
	for _, v := range r.T {
		if v != nil {
			n++
		}
	}
	return n
}

// MeterReadingImportResult — что создано при загрузке показаний.
type MeterReadingImportResult struct {
	MetersCreated                 int                    `json:"meters_created"`
	Readings                      int                    `json:"readings"`
	EstimatedReadings             int                    `json:"estimated_readings"`
	SkippedExisting               int                    `json:"skipped_existing"`
	SkippedNoHistory              int                    `json:"skipped_no_history"`
	SkippedNoHistoryMeters        []string               `json:"skipped_no_history_meters,omitempty"`
	LowerReadingEstimates         []LowerReadingEstimate `json:"lower_reading_estimates,omitempty"`
	HighUsageWarnings             []HighUsageWarning     `json:"high_usage_warnings,omitempty"`
	Issues                        []MeterImportIssue     `json:"issues,omitempty"`
	RequiresHighUsageConfirmation bool                   `json:"-"`
}

// MeterImportIssue — строка выгрузки, которая пропущена или требует внимания.
type MeterImportIssue struct {
	Row     int    `json:"row,omitempty"`
	Serial  string `json:"serial,omitempty"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type MeterImportHistoryItem struct {
	ID         int64                    `json:"id"`
	BuildingID int64                    `json:"building_id"`
	FileName   string                   `json:"file_name"`
	Period     Date                     `json:"period"`
	CreatedAt  time.Time                `json:"created_at"`
	Result     MeterReadingImportResult `json:"result"`
	Issues     []MeterImportIssue       `json:"issues"`
}

// LowerReadingEstimate — показание файла ниже последнего фактического, вместо него записано среднее.
type LowerReadingEstimate struct {
	MeterSerial     string  `json:"meter_serial"`
	Premises        string  `json:"premises"`
	PreviousReading float64 `json:"previous_reading"`
	FileReading     float64 `json:"file_reading"`
	AppliedReading  float64 `json:"applied_reading"`
}

// HighUsageWarning — расход за месяц превысил втрое расход предыдущего месяца.
type HighUsageWarning struct {
	MeterSerial   string  `json:"meter_serial"`
	Premises      string  `json:"premises"`
	PreviousUsage float64 `json:"previous_usage"`
	CurrentUsage  float64 `json:"current_usage"`
}

// UM31ReadingImportRow — показание из файла УМ-31RTU.
type UM31ReadingImportRow struct {
	Row         int
	Serial      string
	ReadingDate Date
	Total       *float64
	T           [3]*float64
}
