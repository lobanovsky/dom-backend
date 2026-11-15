package store

import (
	"context"
	"strings"
	"unicode"

	"dom-backend/internal/model"
	"github.com/jackc/pgx/v5"
)

func (s *Gate) ListVehicles(ctx context.Context, person int64) ([]model.GateVehicleRecord, error) {
	return collect[model.GateVehicleRecord](s.pool.Query(ctx, `SELECT id,person_id,plate_number,brand,active FROM gate_vehicles WHERE person_id=$1 AND active AND deleted_at IS NULL ORDER BY plate_number,id`, person))
}

func (s *Gate) SaveVehicle(ctx context.Context, person, id int64, in model.GateVehicleRecordInput) (model.GateVehicleRecord, error) {
	var zero model.GateVehicleRecord
	in.Normalize()
	if err := in.Validate(); err != nil {
		return zero, err
	}
	var property bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM premises p WHERE `+residentOwns+` AND p.kind IN('apartment','commercial','parking_space'))`, person).Scan(&property); err != nil {
		return zero, mapErr(err)
	}
	if !property {
		return zero, invalid("gate: property is not eligible")
	}
	if id == 0 {
		return one[model.GateVehicleRecord](s.pool.Query(ctx, `INSERT INTO gate_vehicles(person_id,plate_number,plate_key,brand) VALUES($1,$2,regexp_replace(upper($2),'[[:space:]-]','','g'),$3) RETURNING id,person_id,plate_number,brand,active`, person, in.PlateNumber, in.Brand))
	}
	return one[model.GateVehicleRecord](s.pool.Query(ctx, `UPDATE gate_vehicles SET plate_number=$3,plate_key=regexp_replace(upper($3),'[[:space:]-]','','g'),brand=$4,updated_at=now() WHERE id=$1 AND person_id=$2 AND active AND deleted_at IS NULL RETURNING id,person_id,plate_number,brand,active`, id, person, in.PlateNumber, in.Brand))
}

func (s *Gate) ArchiveVehicle(ctx context.Context, person, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE gate_vehicles SET active=false,updated_at=now() WHERE id=$1 AND person_id=$2 AND active AND deleted_at IS NULL`, id, person)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func linkGateVehicles(ctx context.Context, tx pgx.Tx, requestID, person int64, ids []int64, replace bool) error {
	if replace {
		if _, err := tx.Exec(ctx, `DELETE FROM gate_request_vehicles rv USING gate_vehicles v WHERE rv.vehicle_id=v.id AND rv.request_id=$1 AND v.active`, requestID); err != nil {
			return mapErr(err)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > 10 {
		return invalid("gate: at most 10 cars are allowed")
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return invalid("gate: vehicle IDs must be unique positive IDs")
		}
		seen[id] = true
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM gate_vehicles WHERE id=ANY($1::bigint[]) AND person_id=$2 AND active AND deleted_at IS NULL`, ids, person).Scan(&count); err != nil {
		return mapErr(err)
	}
	if count != len(ids) {
		return invalid("gate: car is not available")
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO gate_request_vehicles(request_id,vehicle_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, requestID, id); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

func (s *Gate) AdminVehicle(ctx context.Context, id int64, in model.GateVehicleRecordInput) (model.GateVehicleRecord, error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return model.GateVehicleRecord{}, err
	}
	return one[model.GateVehicleRecord](s.pool.Query(ctx, `UPDATE gate_vehicles SET plate_number=$2,plate_key=regexp_replace(upper($2),'[[:space:]-]','','g'),brand=$3,updated_at=now() WHERE id=$1 AND active AND deleted_at IS NULL RETURNING id,person_id,plate_number,brand,active`, id, in.PlateNumber, in.Brand))
}

func (s *Gate) AdminArchiveVehicle(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE gate_vehicles SET active=false,updated_at=now() WHERE id=$1 AND active AND deleted_at IS NULL`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func saveAndLinkAdminVehicles(ctx context.Context, tx pgx.Tx, requestIDs []int64, person int64, cars []model.GateVehicleInput) error {
	for _, car := range cars {
		key := strings.Map(func(r rune) rune {
			if r == '-' || r == ' ' || r == '\t' {
				return -1
			}
			return unicode.ToUpper(r)
		}, car.PlateNumber)
		var id int64
		if key == "" {
			if err := tx.QueryRow(ctx, `INSERT INTO gate_vehicles(person_id,plate_number,plate_key,brand) VALUES($1,$2,'',$3) RETURNING id`, person, car.PlateNumber, car.Brand).Scan(&id); err != nil {
				return mapErr(err)
			}
		} else if err := tx.QueryRow(ctx, `INSERT INTO gate_vehicles(person_id,plate_number,plate_key,brand) VALUES($1,$2,$3,$4) ON CONFLICT(person_id,plate_key) WHERE plate_key<>'' AND deleted_at IS NULL DO UPDATE SET plate_number=EXCLUDED.plate_number,brand=EXCLUDED.brand,active=true,updated_at=now() RETURNING id`, person, car.PlateNumber, key, car.Brand).Scan(&id); err != nil {
			return mapErr(err)
		}
		for _, requestID := range requestIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO gate_request_vehicles(request_id,vehicle_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, requestID, id); err != nil {
				return mapErr(err)
			}
		}
	}
	return nil
}
