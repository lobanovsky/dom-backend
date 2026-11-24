package store

import (
	"context"
	"math"

	"dom-backend/internal/model"
)

const waterBalanceInletSerial = "146301425"

func (s *Water) waterBalance(ctx context.Context, period model.Date) (model.WaterBalance, error) {
	var result model.WaterBalance
	err := s.pool.QueryRow(ctx, `
WITH inlet AS (
 SELECT id,building_id FROM meters
 WHERE resource='cold_water' AND serial_number=$2 AND deleted_at IS NULL AND building_id IS NOT NULL
), selected AS (
 SELECT m.*, CASE p.kind
  WHEN 'apartment' THEN 'Квартира № ' || p.number
  WHEN 'commercial' THEN 'Офис № ' || p.number
  ELSE COALESCE(m.name,'Общедомовой счётчик') END AS location, CASE
  WHEN m.id IN (SELECT id FROM inlet) THEN 'inlet'
  WHEN p.id IS NULL THEN 'common'
  ELSE p.kind || '_' || m.resource END AS category
 FROM meters m LEFT JOIN premises p ON p.id=m.premises_id
 WHERE m.deleted_at IS NULL AND m.resource IN ('cold_water','hot_water')
 AND m.installed_at < ($1::date+interval '1 month') AND (m.removed_at IS NULL OR m.removed_at >= $1)
 AND (
  (p.deleted_at IS NULL AND p.kind IN ('apartment','commercial') AND p.building_id IN (SELECT building_id FROM inlet))
  OR (m.building_id IN (SELECT building_id FROM inlet) AND (m.id IN (SELECT id FROM inlet) OR m.serial_number IN ('864041','145558')))
 )
), volumes AS (
 SELECT m.category, m.id, m.serial_number AS serial, m.resource, m.location,
 COALESCE(prev.t1,m.initial_t1) AS previous, r.t1 AS current,
 CASE WHEN prev.t1 IS NOT NULL OR m.installed_at>=($1::date-interval '1 month')::date
 THEN (r.t1-COALESCE(prev.t1,m.initial_t1))*m.coefficient END AS volume
 FROM selected m
 LEFT JOIN LATERAL (
  SELECT t1 FROM meter_readings WHERE meter_id=m.id AND period=$1 AND deleted_at IS NULL
  ORDER BY reading_date DESC,id DESC LIMIT 1
 ) r ON true
 LEFT JOIN LATERAL (
  SELECT t1 FROM meter_readings WHERE meter_id=m.id AND period<$1 AND deleted_at IS NULL
  ORDER BY period DESC,reading_date DESC,id DESC LIMIT 1
 ) prev ON true
), totals AS (
 SELECT category, CASE WHEN count(*)=count(volume) AND min(volume)>=0
 AND (category NOT IN ('common','inlet') OR count(*)=CASE WHEN category='common' THEN 2 ELSE 1 END)
 THEN sum(volume) END AS volume
 FROM volumes GROUP BY category
)
SELECT (SELECT volume FROM totals WHERE category='apartment_hot_water'),
 (SELECT volume FROM totals WHERE category='apartment_cold_water'),
 (SELECT volume FROM totals WHERE category='commercial_cold_water'),
 (SELECT volume FROM totals WHERE category='commercial_hot_water'),
 (SELECT volume FROM totals WHERE category='inlet'),
 (SELECT volume FROM totals WHERE category='common'),
 (SELECT COALESCE(jsonb_agg(to_jsonb(v) - 'category' ORDER BY location, resource, serial), '[]'::jsonb)
  FROM volumes v WHERE volume < 0)`, period, waterBalanceInletSerial).Scan(
		&result.ApartmentHot, &result.ApartmentCold, &result.OfficeCold, &result.OfficeHot, &result.Inlet, &result.CommonMeters, &result.NegativeMeters,
	)
	if err != nil {
		return result, err
	}
	if result.Inlet != nil && result.ApartmentHot != nil && result.ApartmentCold != nil &&
		result.OfficeCold != nil && result.OfficeHot != nil && result.CommonMeters != nil {
		value := *result.Inlet - *result.ApartmentHot - *result.ApartmentCold -
			*result.OfficeCold - *result.OfficeHot - *result.CommonMeters
		value = math.Round(value*1000) / 1000
		result.CommonNeeds = &value
	}
	return result, nil
}
