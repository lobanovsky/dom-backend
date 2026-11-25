package store

import (
	"context"
	"math"

	"dom-backend/internal/model"
)

func (s *Electricity) electricityOperations(ctx context.Context, period model.Date) (model.ElectricityOperations, error) {
	var result model.ElectricityOperations
	rows, err := s.pool.Query(ctx, `
WITH common AS (
 SELECT id, building_id, serial_number FROM meters
 WHERE building_id IS NOT NULL AND deleted_at IS NULL AND resource='electricity'
 AND serial_number IN ('023260208838','023260210672','023260209481','023260210866')
), selected AS (
 SELECT id, CASE WHEN serial_number IN ('023260208838','023260210672') THEN 0 ELSE 1 END AS group_id FROM common
 UNION ALL
 SELECT id, 3 FROM meters
 WHERE building_id IS NOT NULL AND deleted_at IS NULL AND resource='electricity'
 AND serial_number IN ('023260210667','023260210792')
 UNION ALL
 SELECT m.id, CASE WHEN m.serial_number='023260085002' THEN 4 ELSE 5 END
 FROM meters m JOIN premises p ON p.id=m.premises_id
 WHERE p.deleted_at IS NULL AND p.kind='commercial' AND m.deleted_at IS NULL AND m.resource='electricity'
 AND m.serial_number IN ('023260085002','023260084425')
 UNION ALL
 SELECT m.id, 2 FROM meters m JOIN premises p ON p.id=m.premises_id
 WHERE p.deleted_at IS NULL AND p.kind='apartment' AND m.deleted_at IS NULL AND m.resource='electricity'
 AND p.building_id IN (SELECT building_id FROM common)
 AND m.installed_at<($1::date+interval '1 month') AND (m.removed_at IS NULL OR m.removed_at >= $1)
)
SELECT selected.group_id, m.tariffs,
 (r.t1-COALESCE(prev.t1,m.initial_t1))*m.coefficient,
 (r.t2-COALESCE(prev.t2,m.initial_t2))*m.coefficient,
 (r.t3-COALESCE(prev.t3,m.initial_t3))*m.coefficient
FROM selected JOIN meters m ON m.id=selected.id
LEFT JOIN LATERAL (
 SELECT t1,t2,t3,reading_date FROM meter_readings
 WHERE meter_id=m.id AND deleted_at IS NULL AND period=$1
 AND m.installed_at<($1::date+interval '1 month') AND (m.removed_at IS NULL OR m.removed_at >= $1)
 ORDER BY reading_date DESC,id DESC LIMIT 1
) r ON true
LEFT JOIN LATERAL (
 SELECT t1,t2,t3 FROM meter_readings
 WHERE meter_id=m.id AND deleted_at IS NULL AND reading_date<r.reading_date
 ORDER BY reading_date DESC,id DESC LIMIT 1
) prev ON true`, period)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	groups := [...]*model.ElectricityVolumeGroup{
		&result.House, &result.Heating, &result.Apartments, &result.Parking,
	}
	for rows.Next() {
		var group, tariffs int
		var values [3]*float64
		if err := rows.Scan(&group, &tariffs, &values[0], &values[1], &values[2]); err != nil {
			return result, err
		}
		if group >= len(groups) {
			result.Offices[group-len(groups)] = values[0]
			continue
		}
		target := groups[group]
		target.Meters++
		for i := range values {
			if i >= tariffs && group == 2 {
				continue
			}
			if values[i] == nil {
				target.Missing[i]++
			} else {
				target.Volume[i] += *values[i]
			}
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	for i := range result.Billable {
		if result.House.Meters == 2 && result.Heating.Meters == 2 && result.Apartments.Meters > 0 &&
			result.House.Missing[i]+result.Heating.Missing[i]+result.Apartments.Missing[i] == 0 {
			value := result.House.Volume[i] + result.Heating.Volume[i] - result.Apartments.Volume[i]
			value = math.Round(value*1000) / 1000
			result.Billable[i] = &value
		}
	}
	return result, nil
}
