package store

import (
	"context"
	"math"

	"dom-backend/internal/model"
)

func (s *Water) waterResidentCharges(ctx context.Context, period model.Date, balance model.WaterBalance) (model.WaterResidentCharges, error) {
	var result model.WaterResidentCharges
	var hotBase, hotHeating *float64
	err := s.pool.QueryRow(ctx, `
WITH prices AS (
 SELECT CASE
  WHEN meter_resource='cold_water' AND lower(name)='хвс' THEN 'cold'
  WHEN meter_resource='hot_water' AND lower(name)='хвс для гвс' THEN 'hot_base'
  WHEN meter_resource='hot_water' AND lower(name) IN ('подогев хвс для гвс','подогрев хвс для гвс') THEN 'hot_heating'
  WHEN calc_method='sum_volumes' AND lower(name)='водоотведение' THEN 'sewer'
 END AS kind, service_unit_price_on(id,$1) AS price
 FROM services
 WHERE deleted_at IS NULL AND active AND unit='m3'
 AND organization_id IN (
  SELECT b.organization_id FROM meters m JOIN buildings b ON b.id=m.building_id
  WHERE m.resource='cold_water' AND m.serial_number=$2 AND m.deleted_at IS NULL
 )
), unique_prices AS (
 SELECT kind, min(price) AS price FROM prices GROUP BY kind HAVING count(*)=count(price) AND min(price)=max(price)
)
SELECT (SELECT price FROM unique_prices WHERE kind='cold'),
 (SELECT price FROM unique_prices WHERE kind='hot_base'),
 (SELECT price FROM unique_prices WHERE kind='hot_heating'),
 (SELECT price FROM unique_prices WHERE kind='sewer')`, period, waterBalanceInletSerial).Scan(
		&result.ColdPrice, &hotBase, &hotHeating, &result.SewerPrice,
	)
	if err != nil {
		return result, err
	}
	if hotBase != nil && hotHeating != nil {
		price := *hotBase + *hotHeating
		result.HotPrice = &price
	}
	charges := func(cold, hot *float64) model.WaterResidentAmounts {
		var out model.WaterResidentAmounts
		amount := func(volume, price *float64) *float64 {
			if volume == nil || price == nil {
				return nil
			}
			value := math.Round(*volume**price*100) / 100
			return &value
		}
		out.Cold = amount(cold, result.ColdPrice)
		out.Hot = amount(hot, result.HotPrice)
		if cold != nil && hot != nil {
			volume := *cold + *hot
			out.Sewer = amount(&volume, result.SewerPrice)
		}
		if out.Cold != nil && out.Hot != nil && out.Sewer != nil {
			total := math.Round((*out.Cold+*out.Hot+*out.Sewer)*100) / 100
			out.Total = &total
		}
		return out
	}
	result.Apartments = charges(balance.ApartmentCold, balance.ApartmentHot)
	result.Offices = charges(balance.OfficeCold, balance.OfficeHot)
	if result.Apartments.Total != nil && result.Offices.Total != nil {
		total := math.Round((*result.Apartments.Total+*result.Offices.Total)*100) / 100
		result.Total = &total
	}
	return result, nil
}
