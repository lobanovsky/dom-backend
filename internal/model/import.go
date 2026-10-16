package model

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
	MetersCreated int `json:"meters_created"`
	Readings      int `json:"readings"`
}
