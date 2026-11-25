package store

import (
	"context"

	"dom-backend/internal/model"
)

const waterInletSerial = "146301425"

func (s *Water) waterVolumes(ctx context.Context, period model.Date) (model.WaterVolumes, error) {
	var result model.WaterVolumes
	err := s.pool.QueryRow(ctx, `
WITH inlet AS (
 SELECT id,building_id FROM meters
 WHERE resource='cold_water' AND serial_number=$2 AND deleted_at IS NULL AND building_id IS NOT NULL
), selected AS (
 SELECT m.*, CASE WHEN m.premises_id IS NULL THEN 'parking'
 ELSE p.kind || '_' || m.resource END AS category
 FROM meters m LEFT JOIN premises p ON p.id=m.premises_id
 WHERE m.deleted_at IS NULL AND m.resource IN ('cold_water','hot_water')
 AND m.installed_at < ($1::date+interval '1 month') AND (m.removed_at IS NULL OR m.removed_at >= $1)
 AND (
  (p.deleted_at IS NULL AND p.kind IN ('apartment','commercial') AND p.building_id IN (SELECT building_id FROM inlet))
  OR (m.building_id IN (SELECT building_id FROM inlet) AND m.resource='cold_water' AND m.serial_number='145558')
 )
), volumes AS (
 SELECT m.category,
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
 SELECT category, CASE WHEN count(*)=count(volume) THEN sum(volume) END AS volume,
 CASE WHEN count(*)=count(volume) THEN sum(greatest(volume,0)) END AS nonnegative_volume,
 CASE WHEN count(*)=count(volume) AND min(volume)>=0 THEN sum(volume) END AS report_volume
 FROM volumes GROUP BY category
)
SELECT (SELECT volume FROM totals WHERE category='apartment_hot_water'),
 (SELECT volume FROM totals WHERE category='apartment_cold_water'),
 (SELECT volume FROM totals WHERE category='commercial_cold_water'),
 (SELECT volume FROM totals WHERE category='commercial_hot_water'),
 (SELECT volume FROM totals WHERE category='parking'),
 (SELECT nonnegative_volume FROM totals WHERE category='apartment_hot_water'),
 (SELECT report_volume FROM totals WHERE category='commercial_hot_water')`, period, waterInletSerial).Scan(
		&result.ApartmentHot, &result.ApartmentCold, &result.OfficeCold, &result.OfficeHot, &result.ParkingCold,
		&result.ReportApartmentHot, &result.ReportOfficeHot,
	)
	return result, err
}
