package model

import (
	"math"
	"strings"
)

var (
	MeterResources      = []string{"cold_water", "hot_water", "electricity"}
	MeterReadingSources = []string{"manual", "import"}
)

// Meter — счётчик воды или электроэнергии. Индивидуальный привязан к помещению (PremisesID),
// общедомовой — к дому (BuildingID); задано ровно одно из двух.
// Активность, просроченная поверка, адрес и последнее показание вычисляются сервером и при записи игнорируются.
type Meter struct {
	Meta
	BuildingID         *int64  `json:"building_id" db:"building_id"`
	PremisesID         *int64  `json:"premises_id" db:"premises_id"`
	Resource           string  `json:"resource" db:"resource"`
	Tariffs            int     `json:"tariffs" db:"tariffs"` // у электросчётчика 1–3, у водяного всегда 1
	SerialNumber       string  `json:"serial_number" db:"serial_number"`
	Name               *string `json:"name" db:"name"` // название для различения счётчиков (например, «ИТП»); не уникально
	Model              *string `json:"model" db:"model"`
	Comment            *string `json:"comment" db:"comment"`
	Coefficient        float64 `json:"coefficient" db:"coefficient"` // коэффициент трансформации: расход = разность показаний × коэффициент
	InstalledAt        Date    `json:"installed_at" db:"installed_at"`
	RemovedAt          *Date   `json:"removed_at" db:"removed_at"`
	VerifiedAt         *Date   `json:"verified_at" db:"verified_at"`
	NextVerificationAt *Date   `json:"next_verification_at" db:"next_verification_at"`
	InitialT1          float64 `json:"initial_t1" db:"initial_t1"`
	InitialT2          float64 `json:"initial_t2" db:"initial_t2"`
	InitialT3          float64 `json:"initial_t3" db:"initial_t3"`

	Active              bool     `json:"active" db:"active"`
	VerificationOverdue bool     `json:"verification_overdue" db:"verification_overdue"`
	LocationBuildingID  int64    `json:"location_building_id" db:"location_building_id"` // дом счётчика, в том числе через помещение
	BuildingAddress     string   `json:"building_address" db:"building_address"`
	PremisesKind        *string  `json:"premises_kind" db:"premises_kind"`
	PremisesNumber      *string  `json:"premises_number" db:"premises_number"`
	LastReadingDate     *Date    `json:"last_reading_date" db:"last_reading_date"`
	LastT1              *float64 `json:"last_t1" db:"last_t1"`
	LastT2              *float64 `json:"last_t2" db:"last_t2"`
	LastT3              *float64 `json:"last_t3" db:"last_t3"`
}

// SetDefaults подставляет значения по умолчанию и обнуляет начальные показания неиспользуемых тарифов.
func (m *Meter) SetDefaults() {
	if m.Tariffs == 0 {
		m.Tariffs = 1
	}
	if m.Coefficient == 0 {
		m.Coefficient = 1
	}
	if m.Tariffs < 2 {
		m.InitialT2 = 0
	}
	if m.Tariffs < 3 {
		m.InitialT3 = 0
	}
}

func (m Meter) Validate() error {
	var scope error
	if (m.BuildingID == nil) == (m.PremisesID == nil) {
		scope = invalid("premises_id", "exactly one of premises_id and building_id must be set")
	}
	var tariffs error
	switch {
	case m.Tariffs < 1 || m.Tariffs > 3:
		tariffs = invalid("tariffs", "must be between 1 and 3")
	case m.Resource != "electricity" && m.Tariffs != 1:
		tariffs = invalid("tariffs", "must be 1 for water meters")
	}
	var name error
	if m.Name != nil && strings.TrimSpace(*m.Name) == "" {
		name = invalid("name", "must not be blank")
	}
	var removed, next error
	if m.RemovedAt != nil && m.RemovedAt.Before(m.InstalledAt.Time) {
		removed = invalid("removed_at", "must not be before installed_at")
	}
	if m.NextVerificationAt != nil && m.VerifiedAt != nil && m.NextVerificationAt.Before(m.VerifiedAt.Time) {
		next = invalid("next_verification_at", "must not be before verified_at")
	}
	var coefficient error
	if m.Coefficient <= 0 {
		coefficient = invalid("coefficient", "must be positive")
	} else {
		coefficient = decimals("coefficient", m.Coefficient, 3)
	}
	return firstErr(
		scope,
		oneOf("resource", m.Resource, MeterResources),
		tariffs,
		required("serial_number", m.SerialNumber),
		name,
		coefficient,
		requiredDate("installed_at", m.InstalledAt),
		removed,
		next,
		reading("initial_t1", m.InitialT1),
		reading("initial_t2", m.InitialT2),
		reading("initial_t3", m.InitialT3),
	)
}

// MeterReading — показания счётчика на дату. T2 и T3 заполняются по числу тарифов счётчика (это проверяет хранилище).
// DeltaT* — расход с предыдущего показания (для первого — с начальных показаний счётчика) с учётом коэффициента;
// считает сервер, при записи игнорируется.
type MeterReading struct {
	Meta
	MeterID     int64    `json:"meter_id" db:"meter_id"`
	ReadingDate Date     `json:"reading_date" db:"reading_date"`
	Period      Date     `json:"period" db:"period"` // расчётный месяц (первое число); пусто — месяц даты показаний
	T1          float64  `json:"t1" db:"t1"`
	T2          *float64 `json:"t2" db:"t2"`
	T3          *float64 `json:"t3" db:"t3"`
	Source      string   `json:"source" db:"source"`
	Comment     *string  `json:"comment" db:"comment"`

	DeltaT1 *float64 `json:"delta_t1" db:"delta_t1"`
	DeltaT2 *float64 `json:"delta_t2" db:"delta_t2"`
	DeltaT3 *float64 `json:"delta_t3" db:"delta_t3"`
}

func (r *MeterReading) SetDefaults() {
	if r.Period.IsZero() {
		r.Period = r.ReadingDate.FirstOfMonth()
	} else {
		r.Period = r.Period.FirstOfMonth()
	}
	if r.Source == "" {
		r.Source = "manual"
	}
}

func (r MeterReading) Validate() error {
	var t2, t3 error
	if r.T2 != nil {
		t2 = reading("t2", *r.T2)
	}
	if r.T3 != nil {
		t3 = reading("t3", *r.T3)
	}
	return firstErr(
		positiveInt("meter_id", intPtr(r.MeterID)),
		requiredDate("reading_date", r.ReadingDate),
		reading("t1", r.T1),
		t2, t3,
		oneOf("source", r.Source, MeterReadingSources),
	)
}

// Milli переводит показание в тысячные доли: сравнивать дробные числа надо как целые.
func Milli(v float64) int64 { return int64(math.Round(v * 1000)) }

// reading: показание не отрицательно и имеет не более трёх знаков после запятой.
func reading(field string, v float64) error {
	if v < 0 {
		return invalid(field, "must not be negative")
	}
	return decimals(field, v, 3)
}

func decimals(field string, v float64, n int) error {
	k := math.Pow10(n)
	if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v*k-math.Round(v*k)) > 1e-6 {
		return invalid(field, "must have at most %d decimal places", n)
	}
	return nil
}
