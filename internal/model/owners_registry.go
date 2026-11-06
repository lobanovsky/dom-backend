package model

// OwnersRegistry — реестр собственников дома на дату: данные для PDF.
type OwnersRegistry struct {
	Address string
	Day     string // YYYY-MM-DD, на какую дату действующие собственники
	Rows    []OwnersRegistryRow
}

type OwnersRegistryRow struct {
	Label     string   // «кв.001», «мм.140», «оф.003»
	Area      *float64 // общая площадь помещения, м²
	Owners    []string // ФИО физлиц и названия юрлиц, действующих на Day
	Cadastral string
}

// TotalArea — общая площадь помещений дома (у помещений без площади — ноль).
func (r OwnersRegistry) TotalArea() float64 {
	var sum float64
	for _, row := range r.Rows {
		if row.Area != nil {
			sum += *row.Area
		}
	}
	return sum
}
