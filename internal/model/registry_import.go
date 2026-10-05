package model

// RegistryPayment — платёж из строки реестра Сбера. Суммы в копейках: так итоговую строку
// можно сверять точно, без ошибок округления float.
type RegistryPayment struct {
	Line        int // номер строки в файле (с 1)
	Date        Date
	Time        Clock
	ExternalID  string // номер операции
	AccountNum  string // номер лицевого счёта
	PayerName   string
	Address     string
	Amount      int64
	Transferred int64
	Commission  int64
	Raw         string
}

// ParsedRegistry — разобранный файл реестра.
type ParsedRegistry struct {
	FileAccount      string // расчётный счёт из имени файла; пусто, если имя нестандартное
	RegistryNumber   string
	RegistryDate     *Date
	Payments         []RegistryPayment
	TotalAmount      int64
	TotalTransferred int64
	TotalCommission  int64
}

// SkippedPayment — платёж, который не загружен, потому что номер операции уже известен.
type SkippedPayment struct {
	ExternalID  string  `json:"external_id"`
	PaymentDate Date    `json:"payment_date"`
	PayerName   string  `json:"payer_name"`
	Amount      float64 `json:"amount"`
}

// RegistryImportResult — итог загрузки реестра.
type RegistryImportResult struct {
	RegistryID        int64            `json:"registry_id"`
	Created           int              `json:"created"`
	SkippedDuplicates int              `json:"skipped_duplicates"`
	Linked            int              `json:"linked"`
	Unlinked          int              `json:"unlinked"`
	Skipped           []SkippedPayment `json:"skipped"`
	Warnings          []string         `json:"warnings"`
}
