package store

import (
	"context"
	"math"

	"dom-backend/internal/model"
)

func (s *Electricity) electricityOperations(ctx context.Context, period model.Date) (model.ElectricityOperations, error) {
	result := model.ElectricityOperations{CommercialPremises: []model.CommercialElectricityVolume{}}
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
 SELECT id, 5 FROM meters
 WHERE building_id IS NOT NULL AND deleted_at IS NULL AND resource='electricity'
 AND serial_number IN ('023260084425','023260085002')
 UNION ALL
 SELECT m.id, CASE WHEN p.kind='apartment' THEN 2 ELSE 4 END FROM meters m JOIN premises p ON p.id=m.premises_id
 WHERE p.deleted_at IS NULL AND p.kind IN ('apartment','commercial') AND m.deleted_at IS NULL AND m.resource='electricity'
 AND p.building_id IN (SELECT building_id FROM common)
 AND m.installed_at<($1::date+interval '1 month') AND (m.removed_at IS NULL OR m.removed_at >= $1)
)
SELECT selected.group_id, m.tariffs, COALESCE(p.id,0), COALESCE(p.number,''), COALESCE(b.name,b.address,''),
 (r.t1-COALESCE(prev.t1,m.initial_t1))*m.coefficient,
 (r.t2-COALESCE(prev.t2,m.initial_t2))*m.coefficient,
 (r.t3-COALESCE(prev.t3,m.initial_t3))*m.coefficient
FROM selected JOIN meters m ON m.id=selected.id
LEFT JOIN premises p ON p.id=m.premises_id
LEFT JOIN buildings b ON b.id=p.building_id
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
) prev ON true
ORDER BY b.address, substring(p.number from '\d+')::numeric, p.number, p.id, m.id`, period)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	groups := [...]*model.ElectricityVolumeGroup{
		&result.House, &result.Heating, &result.Apartments, &result.Parking,
		&result.CommercialIndividual, &result.CommercialCommon,
	}
	byPremises := make(map[int64]int)
	for rows.Next() {
		var group, tariffs int
		var values [3]*float64
		var premisesID int64
		var number, building string
		if err := rows.Scan(&group, &tariffs, &premisesID, &number, &building, &values[0], &values[1], &values[2]); err != nil {
			return result, err
		}
		if group == 4 {
			index, found := byPremises[premisesID]
			if !found {
				index = len(result.CommercialPremises)
				byPremises[premisesID] = index
				result.CommercialPremises = append(result.CommercialPremises, model.CommercialElectricityVolume{Number: number, Building: building})
			}
			item := &result.CommercialPremises[index]
			if values[0] == nil {
				item.Missing++
			} else {
				item.Volume += *values[0]
			}
		}
		target := groups[group]
		target.Meters++
		for i := range values {
			if i >= tariffs && (group == 2 || group == 4) {
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
