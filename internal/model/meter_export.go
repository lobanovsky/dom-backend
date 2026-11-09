package model

// MeterReadingExport — последнее действующее показание счётчика за расчётный месяц.
type MeterReadingExport struct {
	PremisesKind   string   `db:"premises_kind"`
	PremisesNumber string   `db:"premises_number"`
	SerialNumber   string   `db:"serial_number"`
	T1             float64  `db:"t1"`
	T2             *float64 `db:"t2"`
	T3             *float64 `db:"t3"`
}
