package model

import "slices"

var (
	ServiceKinds       = []string{"utility", "maintenance", "additional"}
	ServiceUnits       = []string{"kwh", "m3", "gcal", "m2", "piece", "month"}
	ServiceCalcMethods = []string{"fixed", "per_area", "individual_meter", "common_meter", "sum", "manual"}
)

// Service — услуга организации: коммунальная, эксплуатационная или дополнительная.
// CalcMethod описывает, как считается объём: по индивидуальному счётчику (MeterResource + MeterTariff), по общедомовому
// счётчику с делением по площади, фиксированно за помещение или за м², суммой других услуг (ComponentIDs) или вводом вручную.
// Цена задаётся тарифами (ServiceTariff); у составной услуги (sum) тарифов нет, цена — сумма цен компонентов.
// Текущая цена и название организации вычисляются сервером и при записи игнорируются.
type Service struct {
	Meta
	OrganizationID int64   `json:"organization_id" db:"organization_id"`
	Kind           string  `json:"kind" db:"kind"`
	Name           string  `json:"name" db:"name"`
	Unit           string  `json:"unit" db:"unit"`
	CalcMethod     string  `json:"calc_method" db:"calc_method"`
	MeterResource  *string `json:"meter_resource" db:"meter_resource"`
	MeterTariff    *int    `json:"meter_tariff" db:"meter_tariff"`
	ComponentIDs   []int64 `json:"component_ids" db:"component_ids"`
	Position       int     `json:"position" db:"position"`
	Active         bool    `json:"active" db:"active"`
	Comment        *string `json:"comment" db:"comment"`

	OrganizationName string   `json:"organization_name" db:"organization_name"`
	CurrentPrice     *float64 `json:"current_price" db:"current_price"` // цена на сегодня; nil — тарифа на сегодня нет
}

func (s *Service) SetDefaults() {
	if s.ComponentIDs == nil {
		s.ComponentIDs = []int64{}
	}
	// Поля счётчика и состав имеют смысл только у своих способов расчёта.
	if s.CalcMethod != "individual_meter" && s.CalcMethod != "common_meter" {
		s.MeterResource, s.MeterTariff = nil, nil
	} else if s.MeterResource != nil && *s.MeterResource != "electricity" && s.MeterTariff == nil {
		one := 1
		s.MeterTariff = &one
	}
	if s.CalcMethod != "sum" {
		s.ComponentIDs = []int64{}
	}
}

func (s Service) Validate() error {
	var meter error
	if s.CalcMethod == "individual_meter" || s.CalcMethod == "common_meter" {
		switch {
		case s.MeterResource == nil:
			meter = invalid("meter_resource", "is required for this calculation method")
		case !slices.Contains(MeterResources, *s.MeterResource):
			meter = oneOf("meter_resource", *s.MeterResource, MeterResources)
		case s.MeterTariff == nil || *s.MeterTariff < 1 || *s.MeterTariff > 3:
			meter = invalid("meter_tariff", "must be between 1 and 3")
		case *s.MeterResource != "electricity" && *s.MeterTariff != 1:
			meter = invalid("meter_tariff", "must be 1 for water meters")
		}
	}
	var unit error
	if s.CalcMethod == "per_area" && s.Unit != "m2" {
		unit = invalid("unit", "must be m2 for a per-area service")
	}
	var components error
	if s.CalcMethod == "sum" && len(s.ComponentIDs) == 0 {
		components = invalid("component_ids", "must contain at least 1 item")
	}
	return firstErr(
		positiveInt("organization_id", intPtr(s.OrganizationID)),
		oneOf("kind", s.Kind, ServiceKinds),
		required("name", s.Name),
		oneOf("unit", s.Unit, ServiceUnits),
		oneOf("calc_method", s.CalcMethod, ServiceCalcMethods),
		meter, unit, components,
	)
}

// ServiceTariff — цена единицы услуги в период [ValidFrom, ValidTo]; пустой ValidTo — действует до сих пор.
// Периоды одной услуги не пересекаются (это проверяет БД).
type ServiceTariff struct {
	Meta
	ServiceID int64   `json:"service_id" db:"service_id"`
	Price     float64 `json:"price" db:"price"`
	ValidFrom Date    `json:"valid_from" db:"valid_from"`
	ValidTo   *Date   `json:"valid_to" db:"valid_to"`
	Comment   *string `json:"comment" db:"comment"`
}

func (t ServiceTariff) Validate() error {
	var period error
	if t.ValidTo != nil && t.ValidTo.Before(t.ValidFrom.Time) {
		period = invalid("valid_to", "must not be before valid_from")
	}
	var price error
	if t.Price < 0 {
		price = invalid("price", "must not be negative")
	} else {
		price = decimals("price", t.Price, 4)
	}
	return firstErr(
		positiveInt("service_id", intPtr(t.ServiceID)),
		price,
		requiredDate("valid_from", t.ValidFrom),
		period,
	)
}
