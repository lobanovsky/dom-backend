package model

// Counterparty — контрагент, с которым были операции по нашим банковским счетам: получатель исходящих платежей
// или плательщик входящих из выписок. Отдельной таблицы нет: список строится запросом по платежам и всегда актуален.
// Контрагент опознаётся по ИНН и наименованию без регистра, кавычек и пробелов: у физлиц, которым платят через банк,
// в ИНН стоит ИНН банка, поэтому по одному ИНН их объединять нельзя.
type Counterparty struct {
	Name          string   `json:"name" db:"name"`
	INN           *string  `json:"inn" db:"inn"`
	Accounts      []string `json:"accounts" db:"accounts"` // расчётные счета (до 5 разных)
	BankName      *string  `json:"bank_name" db:"bank_name"`
	OutgoingCount int      `json:"outgoing_count" db:"outgoing_count"` // мы платили
	OutgoingTotal float64  `json:"outgoing_total" db:"outgoing_total"`
	IncomingCount int      `json:"incoming_count" db:"incoming_count"` // нам платили (платежи из выписок, без строк реестров)
	IncomingTotal float64  `json:"incoming_total" db:"incoming_total"`
	FirstPayment  Date     `json:"first_payment" db:"first_payment"`
	LastPayment   Date     `json:"last_payment" db:"last_payment"`
}

type CounterpartyFilter struct {
	Direction string  // outgoing — только получатели наших платежей, incoming — только плательщики; пусто — все
	Q         *string // подстрока в наименовании, ИНН или номере счёта
}
