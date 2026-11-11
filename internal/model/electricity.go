package model

// ElectricityContract — контракт Мосэнергосбыта и общедомовые счётчики, которые в него входят.
type ElectricityContract struct {
	ID         int64                      `json:"id" db:"id"`
	Name       string                     `json:"name" db:"name"`
	Number     *string                    `json:"number" db:"number"`
	ArchivedAt *Date                      `json:"archived_at" db:"archived_at"`
	MeterIDs   []int64                    `json:"meter_ids" db:"-"`
	Prices     []ElectricityContractPrice `json:"prices" db:"-"`
}

// ElectricityContractPrice — цена по тарифным зонам за период действия.
type ElectricityContractPrice struct {
	ID        int64    `json:"id" db:"id"`
	ValidFrom Date     `json:"valid_from" db:"valid_from"`
	ValidTo   *Date    `json:"valid_to" db:"valid_to"`
	T1        float64  `json:"t1" db:"t1"`
	T2        *float64 `json:"t2" db:"t2"`
	T3        *float64 `json:"t3" db:"t3"`
}

type ElectricityContractUpdate struct {
	Name     *string                    `json:"name"`
	Number   *string                    `json:"number"`
	MeterIDs []int64                    `json:"meter_ids"`
	Prices   []ElectricityContractPrice `json:"prices"`
}

type ElectricityCalculation struct {
	Period    string                      `json:"period"`
	Complete  bool                        `json:"complete"`
	Total     float64                     `json:"total"`
	Contracts []ElectricityContractResult `json:"contracts"`
}

type ElectricityContractResult struct {
	ID       int64                    `json:"id"`
	Name     string                   `json:"name"`
	Complete bool                     `json:"complete"`
	Total    float64                  `json:"total"`
	Meters   []ElectricityMeterResult `json:"meters"`
}

type ElectricityMeterResult struct {
	MeterID     int64    `json:"meter_id"`
	Name        string   `json:"name"`
	Serial      string   `json:"serial_number"`
	Coefficient float64  `json:"coefficient"`
	Tariffs     int      `json:"tariffs"`
	DeltaT1     *float64 `json:"delta_t1"`
	DeltaT2     *float64 `json:"delta_t2"`
	DeltaT3     *float64 `json:"delta_t3"`
	PriceT1     *float64 `json:"price_t1"`
	PriceT2     *float64 `json:"price_t2"`
	PriceT3     *float64 `json:"price_t3"`
	Amount      float64  `json:"amount"`
	Status      string   `json:"status"`
}
