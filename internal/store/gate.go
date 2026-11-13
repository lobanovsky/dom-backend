package store

import (
	"context"
	"strings"
	"unicode"

	"dom-backend/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Gate struct{ pool *pgxpool.Pool }

func NewGate(p *pgxpool.Pool) *Gate { return &Gate{p} }

const gateToday = `(now() AT TIME ZONE 'Europe/Moscow')::date`
const gateKindZone = `((p.kind='apartment' AND g.zone='courtyard') OR (p.kind='parking_space' AND g.zone='parking'))`
const gateEligibility = `o.person_id=g.person_id AND o.premises_id=g.premises_id AND o.deleted_at IS NULL
 AND o.valid_from<=` + gateToday + ` AND (o.valid_to IS NULL OR o.valid_to>=` + gateToday + `)
 AND pe.deleted_at IS NULL AND p.deleted_at IS NULL AND b.deleted_at IS NULL AND org.deleted_at IS NULL AND ` + gateKindZone
const gateView = `WITH base AS (
 SELECT g.id,g.person_id,g.premises_id,p.building_id,
 concat_ws(' ',pe.last_name,pe.first_name,pe.middle_name) applicant_name,b.address,p.number premises_number,p.kind premises_kind,
 g.zone,g.phone,g.recipient_name,g.relation,g.vehicle_plate,g.vehicle_brand,g.occasional_vehicle,g.valid_until,g.comment,g.status,g.review_note,g.created_at,g.updated_at,
 COALESCE((` + gateEligibility + `),false) eligible
 FROM gate_requests g JOIN ownerships o ON o.id=g.ownership_id JOIN persons pe ON pe.id=g.person_id
 JOIN premises p ON p.id=g.premises_id JOIN buildings b ON b.id=p.building_id JOIN organizations org ON org.id=b.organization_id
), gate_view AS (
 SELECT *,CASE WHEN status NOT IN('pending','approved') THEN status WHEN NOT eligible THEN 'ineligible'
 WHEN valid_until<` + gateToday + ` THEN 'expired' ELSE status END effective_status,
 status='approved' AND eligible AND (valid_until IS NULL OR valid_until>=` + gateToday + `) active FROM base
) `

func (s *Gate) Properties(ctx context.Context, person int64) ([]model.GateProperty, error) {
	return collect[model.GateProperty](s.pool.Query(ctx, `SELECT DISTINCT p.id,p.building_id,p.number,p.kind,b.address,
 CASE WHEN p.kind='apartment' THEN 'courtyard' ELSE 'parking' END zone
 FROM premises p JOIN buildings b ON b.id=p.building_id WHERE p.kind IN('apartment','parking_space') AND `+residentOwns+` ORDER BY p.building_id,p.kind,p.number`, person))
}
func (s *Gate) List(ctx context.Context, person int64, f model.GateFilter, limit, offset int) ([]model.GateRequest, error) {
	return collect[model.GateRequest](s.pool.Query(ctx, gateView+`SELECT * FROM gate_view
 WHERE ($1::bigint=0 OR person_id=$1) AND ($2='' OR effective_status=$2) AND ($3='' OR zone=$3)
 AND ($4='' OR concat_ws(' ',recipient_name,applicant_name,phone,vehicle_plate,vehicle_brand,premises_number,address) ILIKE '%'||$4||'%'
 OR ($5<>'' AND phone LIKE '%'||$5||'%')) ORDER BY created_at DESC,id DESC LIMIT $6 OFFSET $7`, person, f.Status, f.Zone, strings.TrimSpace(f.Q), model.NormalizePhone(f.Q), limit, offset))
}
func (s *Gate) get(ctx context.Context, person, id int64) (model.GateRequest, error) {
	return one[model.GateRequest](s.pool.Query(ctx, gateView+`SELECT * FROM gate_view WHERE id=$1 AND ($2::bigint=0 OR person_id=$2)`, id, person))
}
func (s *Gate) Events(ctx context.Context, person, id int64) ([]model.GateEvent, error) {
	if _, err := s.get(ctx, person, id); err != nil {
		return nil, err
	}
	return collect[model.GateEvent](s.pool.Query(ctx, `SELECT id,action,actor_role,note,created_at FROM gate_events WHERE request_id=$1 ORDER BY id`, id))
}
func (s *Gate) Create(ctx context.Context, person int64, in model.GateInput) (model.GateRequest, error) {
	var zero model.GateRequest
	in.Normalize()
	if err := in.Validate(); err != nil {
		return zero, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, mapErr(err)
	}
	defer rollback(ctx, tx)
	// Общая блокировка помещения сериализует проверку повторных заявок.
	var premise int64
	if err := tx.QueryRow(ctx, `SELECT p.id FROM premises p WHERE p.id=$2 AND `+residentOwns+` FOR UPDATE OF p`, person, in.PremisesID).Scan(&premise); err != nil {
		if err == pgx.ErrNoRows {
			return zero, invalid("gate: property is not eligible")
		}
		return zero, mapErr(err)
	}
	var ownership int64
	err = tx.QueryRow(ctx, `SELECT o.id FROM ownerships o JOIN premises p ON p.id=o.premises_id
 JOIN persons pe ON pe.id=o.person_id JOIN buildings b ON b.id=p.building_id JOIN organizations org ON org.id=b.organization_id
 WHERE o.person_id=$1 AND p.id=$2 AND o.deleted_at IS NULL AND pe.deleted_at IS NULL AND p.deleted_at IS NULL
 AND b.deleted_at IS NULL AND org.deleted_at IS NULL AND o.valid_from<=`+gateToday+` AND (o.valid_to IS NULL OR o.valid_to>=`+gateToday+`)
 AND ((p.kind='apartment' AND $3='courtyard') OR (p.kind='parking_space' AND $3='parking')) ORDER BY o.id LIMIT 1 FOR SHARE OF o`, person, in.PremisesID, in.Zone).Scan(&ownership)
	if err == pgx.ErrNoRows {
		return zero, invalid("gate: property is not eligible")
	}
	if err != nil {
		return zero, mapErr(err)
	}
	var valid bool
	if err := tx.QueryRow(ctx, `SELECT $1::date IS NULL OR $1::date>=`+gateToday, in.ValidUntil).Scan(&valid); err != nil {
		return zero, mapErr(err)
	}
	if !valid {
		return zero, invalid("gate: expiry is in the past")
	}
	if err := tx.QueryRow(ctx, gateView+`SELECT EXISTS(SELECT 1 FROM gate_view WHERE person_id=$1 AND premises_id=$2 AND zone=$3 AND phone=$4 AND effective_status IN('pending','approved'))`, person, in.PremisesID, in.Zone, in.Phone).Scan(&valid); err != nil {
		return zero, mapErr(err)
	}
	if valid {
		return zero, &Error{ErrConflict, "gate: duplicate request"}
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO gate_requests(person_id,ownership_id,premises_id,zone,phone,recipient_name,relation,vehicle_plate,vehicle_brand,occasional_vehicle,valid_until,comment)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`, person, ownership, in.PremisesID, in.Zone, in.Phone, in.RecipientName, in.Relation, in.VehiclePlate, in.VehicleBrand, in.OccasionalVehicle, in.ValidUntil, in.Comment).Scan(&id)
	if err != nil {
		return zero, mapErr(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO gate_events(request_id,action,actor_role,actor_person_id) VALUES($1,'submitted','resident',$2)`, id, person); err != nil {
		return zero, mapErr(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, mapErr(err)
	}
	return s.get(ctx, person, id)
}

// Act фиксирует только допустимый переход; выдача в устройство выполняется отдельным этапом.
func (s *Gate) Act(ctx context.Context, person int64, user model.User, id int64, action, note string) (model.GateRequest, error) {
	var zero model.GateRequest
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 500 || strings.IndexFunc(note, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
		return zero, invalid("gate: invalid decision note")
	}
	target := ""
	if person > 0 {
		switch action {
		case "cancel":
			target = "cancelled"
		case "revoke":
			target = "revoked"
		}
	} else if user.Role == model.RoleAdmin || user.Role == model.RoleGuard {
		switch action {
		case "approve":
			target = "approved"
		case "reject":
			target = "rejected"
		case "revoke":
			target = "revoked"
		}
	}
	if target == "" {
		return zero, invalid("gate: invalid action")
	}
	if person == 0 && (action == "reject" || action == "revoke") && note == "" {
		return zero, invalid("gate: decision reason is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, mapErr(err)
	}
	defer rollback(ctx, tx)
	var status string
	var ownership int64
	err = tx.QueryRow(ctx, `SELECT status,ownership_id FROM gate_requests WHERE id=$1 AND ($2::bigint=0 OR person_id=$2) FOR UPDATE`, id, person).Scan(&status, &ownership)
	if err != nil {
		return zero, mapErr(err)
	}
	if (action == "revoke" && status != "approved") || (action != "revoke" && status != "pending") {
		return zero, &Error{ErrConflict, "gate: request status changed"}
	}
	if action == "approve" {
		if _, err = tx.Exec(ctx, `SELECT id FROM ownerships WHERE id=$1 FOR SHARE`, ownership); err != nil {
			return zero, mapErr(err)
		}
		var eligible bool
		if err = tx.QueryRow(ctx, gateView+`SELECT eligible AND (valid_until IS NULL OR valid_until>=`+gateToday+`) FROM gate_view WHERE id=$1`, id).Scan(&eligible); err != nil {
			return zero, mapErr(err)
		}
		if !eligible {
			return zero, invalid("gate: property or expiry is no longer eligible")
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE gate_requests SET status=$2,review_note=$3,updated_at=now() WHERE id=$1`, id, target, note); err != nil {
		return zero, mapErr(err)
	}
	var actorPerson, actorUser *int64
	role := "resident"
	if person > 0 {
		actorPerson = &person
	} else {
		actorUser = &user.ID
		role = user.Role
	}
	if _, err = tx.Exec(ctx, `INSERT INTO gate_events(request_id,action,actor_role,actor_person_id,actor_user_id,note) VALUES($1,$2,$3,$4,$5,$6)`, id, target, role, actorPerson, actorUser, note); err != nil {
		return zero, mapErr(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, mapErr(err)
	}
	return s.get(ctx, person, id)
}
