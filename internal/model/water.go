package model

import "strings"

// WaterContract — договор Мосводоканала; оба тарифа берутся из справочника.
type WaterContract struct {
	ID            int64   `json:"id" db:"id"`
	Name          string  `json:"name" db:"name"`
	Number        *string `json:"number" db:"number"`
	WaterTariffID int64   `json:"water_tariff_id" db:"water_tariff_id"`
	SewerTariffID int64   `json:"sewer_tariff_id" db:"sewer_tariff_id"`
	ArchivedAt    *Date   `json:"archived_at" db:"archived_at"`
	MeterIDs      []int64 `json:"meter_ids" db:"-"`
}

type WaterContractUpdate struct {
	Name          string  `json:"name"`
	Number        *string `json:"number"`
	WaterTariffID int64   `json:"water_tariff_id"`
	SewerTariffID int64   `json:"sewer_tariff_id"`
	MeterIDs      []int64 `json:"meter_ids"`
}

func (v WaterContractUpdate) Validate() error {
	if strings.TrimSpace(v.Name) == "" || len([]rune(v.Name)) > 200 {
		return invalid("name", "must contain 1 to 200 characters")
	}
	if v.Number != nil && len([]rune(*v.Number)) > 200 {
		return invalid("number", "must not exceed 200 characters")
	}
	if v.WaterTariffID <= 0 || v.SewerTariffID <= 0 {
		return invalid("tariff_ids", "water and sewer tariffs are required")
	}
	if len(v.MeterIDs) == 0 || len(v.MeterIDs) > 200 {
		return invalid("meter_ids", "select 1 to 200 common water meters")
	}
	seen := map[int64]bool{}
	for _, id := range v.MeterIDs {
		if id <= 0 || seen[id] {
			return invalid("meter_ids", "must be unique positive IDs")
		}
		seen[id] = true
	}
	return nil
}

type WaterCalculation struct {
	Period      string                `json:"period"`
	Complete    bool                  `json:"complete"`
	WaterAmount float64               `json:"water_amount"`
	SewerAmount float64               `json:"sewer_amount"`
	Total       float64               `json:"total"`
	Contracts   []WaterContractResult `json:"contracts"`
}

type WaterContractResult struct {
	ID          int64              `json:"id"`
	Name        string             `json:"name"`
	Complete    bool               `json:"complete"`
	Volume      float64            `json:"volume"`
	WaterAmount float64            `json:"water_amount"`
	SewerAmount float64            `json:"sewer_amount"`
	Total       float64            `json:"total"`
	Meters      []WaterMeterResult `json:"meters"`
}

type WaterMeterResult struct {
	MeterID     int64    `json:"meter_id" db:"meter_id"`
	Name        string   `json:"name" db:"meter_name"`
	Serial      string   `json:"serial_number" db:"serial_number"`
	Previous    *float64 `json:"previous" db:"previous"`
	Current     *float64 `json:"current" db:"current"`
	Volume      *float64 `json:"volume" db:"volume"`
	WaterPrice  *float64 `json:"water_price" db:"water_price"`
	SewerPrice  *float64 `json:"sewer_price" db:"sewer_price"`
	WaterAmount float64  `json:"water_amount" db:"water_amount"`
	SewerAmount float64  `json:"sewer_amount" db:"sewer_amount"`
	Amount      float64  `json:"amount" db:"amount"`
	Status      string   `json:"status" db:"status"`
}
