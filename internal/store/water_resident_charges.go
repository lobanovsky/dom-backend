package store

import (
	"context"
	"math"

	"dom-backend/internal/model"
)

func (s *Water) waterResidentCharges(ctx context.Context, period model.Date, volumes model.WaterVolumes) (model.WaterResidentCharges, error) {
	var result model.WaterResidentCharges
	err := s.pool.QueryRow(ctx, `
WITH prices AS (
 SELECT CASE
  WHEN meter_resource='cold_water' AND lower(name)='хвс' THEN 'cold'
  WHEN meter_resource='hot_water' AND lower(name)='хвс для гвс' THEN 'hot_base'
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
 (SELECT price FROM unique_prices WHERE kind='sewer')`, period, waterInletSerial).Scan(
		&result.ColdPrice, &result.HotPrice, &result.SewerPrice,
	)
	if err != nil {
		return result, err
	}
	sum := func(values ...*float64) *float64 {
		total := 0.0
		for _, value := range values {
			if value == nil {
				return nil
			}
			total += *value
		}
		return &total
	}
	amount := func(volume, price *float64) *float64 {
		if volume == nil || price == nil {
			return nil
		}
		value := math.Round(*volume**price*100) / 100
		return &value
	}
	result.ParkingCold = volumes.ParkingCold
	result.ColdVolume = sum(volumes.ApartmentCold, volumes.OfficeCold, volumes.ParkingCold)
	result.HotVolume = sum(volumes.ApartmentHot, volumes.OfficeHot)
	result.SewerVolume = sum(result.ColdVolume, result.HotVolume)
	result.Amounts.Cold = amount(result.ColdVolume, result.ColdPrice)
	result.Amounts.Hot = amount(result.HotVolume, result.HotPrice)
	result.Amounts.Sewer = amount(result.SewerVolume, result.SewerPrice)
	result.Total = sum(result.Amounts.Cold, result.Amounts.Hot, result.Amounts.Sewer)
	if result.Total != nil {
		*result.Total = math.Round(*result.Total*100) / 100
	}
	return result, nil
}
