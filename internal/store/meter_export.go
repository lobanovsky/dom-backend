package store

import (
	"context"
	"dom-backend/internal/model"
)

func (s *MeterReadings) Export(ctx context.Context, period model.Date) ([]model.MeterReadingExport, error) {
	return collect[model.MeterReadingExport](s.pool.Query(ctx, `SELECT m.resource,COALESCE(k.name,p.kind) AS premises_kind,p.number AS premises_number,m.serial_number,r.t1,r.t2,r.t3
 FROM meters m JOIN premises p ON p.id=m.premises_id
 JOIN buildings b ON b.id=p.building_id JOIN organizations org ON org.id=b.organization_id
 LEFT JOIN premises_kinds k ON k.code=p.kind
 JOIN LATERAL (SELECT t1,t2,t3 FROM meter_readings WHERE meter_id=m.id AND period=$1 AND deleted_at IS NULL ORDER BY reading_date DESC,id DESC LIMIT 1) r ON true
 WHERE m.deleted_at IS NULL AND p.deleted_at IS NULL AND b.deleted_at IS NULL AND org.deleted_at IS NULL
 ORDER BY b.id,p.kind,length(p.number),p.number,m.resource,m.serial_number,m.id`, period))
}
