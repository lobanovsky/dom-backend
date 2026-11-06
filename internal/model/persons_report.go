package model

// PersonsReport — отчёт «физлица и их недвижимость» на дату: данные для PDF.
type PersonsReport struct {
	Day               string
	MultipleBuildings bool // помещения из нескольких домов: в отчёте указывается адрес
	Persons           []PersonsReportPerson
}

type PersonsReportPerson struct {
	Name  string
	Items []PersonsReportItem
}

type PersonsReportItem struct {
	Label     string // «кв.001», «оф.003», «мм.140»
	Address   string
	Area      *float64 // общая площадь помещения, м²
	ShareNum  int
	ShareDen  int
	Cadastral string
}
