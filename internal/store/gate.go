package store

import (
	"context"
	"slices"
	"strings"
	"unicode"

	"dom-backend/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Gate struct{ pool *pgxpool.Pool }

func NewGate(p *pgxpool.Pool) *Gate { return &Gate{p} }

const gateToday = `(now() AT TIME ZONE 'Europe/Moscow')::date`
const gateKindZone = `((p.kind IN('apartment','commercial') AND g.zone='courtyard') OR (p.kind='parking_space' AND g.zone='parking'))`
const gateEligibility = `o.person_id=g.person_id AND o.premises_id=g.premises_id AND o.deleted_at IS NULL
 AND o.valid_from<=` + gateToday + ` AND (o.valid_to IS NULL OR o.valid_to>=` + gateToday + `)
 AND pe.deleted_at IS NULL AND p.deleted_at IS NULL AND b.deleted_at IS NULL AND org.deleted_at IS NULL AND ` + gateKindZone
const gateView = `WITH base AS (
 SELECT g.id,g.group_id,g.vehicles,g.no_personal_vehicle,g.person_id,g.premises_id,p.building_id,
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
 CASE WHEN p.kind IN('apartment','commercial') THEN 'courtyard' ELSE 'parking' END zone
 FROM premises p JOIN buildings b ON b.id=p.building_id WHERE p.kind IN('apartment','commercial','parking_space') AND `+residentOwns+` ORDER BY p.building_id,p.kind,p.number`, person))
}
func (s *Gate) List(ctx context.Context, person int64, f model.GateFilter, limit, offset int) ([]model.GateRequest, error) {
	return collect[model.GateRequest](s.pool.Query(ctx, gateView+`, presentation AS (
 SELECT group_id,CASE WHEN bool_or(active) THEN 'approved' WHEN bool_and(status='pending') THEN 'pending' ELSE 'rejected' END display_status
 FROM gate_view GROUP BY group_id
 ), matched AS (
 SELECT DISTINCT group_id FROM gate_view JOIN presentation USING(group_id) WHERE ($8='' OR display_status=$8) AND ($1::bigint=0 OR person_id=$1) AND ($2='' OR effective_status=$2) AND ($3='' OR zone=$3)
 AND ($4='' OR concat_ws(' ',recipient_name,applicant_name,phone,vehicle_plate,vehicle_brand,premises_number,address) ILIKE '%'||$4||'%'
 OR ($5<>'' AND phone LIKE '%'||$5||'%')
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(vehicles) car WHERE COALESCE((car->>'active')::boolean,true) AND concat_ws(' ',car->>'plate_number',car->>'brand') ILIKE '%'||$4||'%'))
 AND ($9='' OR recipient_name ILIKE '%'||$9||'%' OR applicant_name ILIKE '%'||$9||'%')
 AND ($10='' OR phone LIKE '%'||regexp_replace($10,'[^0-9]','','g')||'%')
 AND ($11='' OR vehicle_plate ILIKE '%'||$11||'%' OR vehicle_brand ILIKE '%'||$11||'%' OR EXISTS(SELECT 1 FROM jsonb_array_elements(vehicles) car WHERE COALESCE((car->>'active')::boolean,true) AND concat_ws(' ',car->>'plate_number',car->>'brand') ILIKE '%'||$11||'%'))
 AND ($12='' OR premises_number ILIKE '%'||$12||'%' OR address ILIKE '%'||$12||'%')
 AND ($13::bigint=0 OR premises_id=$13)
 ), selected_groups AS (
 SELECT g.group_id FROM gate_view g JOIN matched m USING(group_id) GROUP BY g.group_id
 ORDER BY max(g.created_at) DESC,g.group_id DESC LIMIT $6 OFFSET $7
 ) SELECT * FROM gate_view WHERE group_id IN(SELECT group_id FROM selected_groups)
 ORDER BY created_at DESC,group_id DESC,id`, person, f.Status, f.Zone, strings.TrimSpace(f.Q), model.NormalizePhone(f.Q), limit, offset, f.DisplayStatus, strings.TrimSpace(f.Recipient), strings.TrimSpace(f.Phone), strings.TrimSpace(f.Vehicle), strings.TrimSpace(f.Premises), f.PremisesID))
}
func (s *Gate) get(ctx context.Context, person, id int64) (model.GateRequest, error) {
	return one[model.GateRequest](s.pool.Query(ctx, gateView+`SELECT * FROM gate_view WHERE id=$1 AND ($2::bigint=0 OR person_id=$2)`, id, person))
}
func (s *Gate) Events(ctx context.Context, person, id int64) ([]model.GateEvent, error) {
	if _, err := s.get(ctx, person, id); err != nil {
		return nil, err
	}
	return collect[model.GateEvent](s.pool.Query(ctx, `SELECT id,action,actor_role,note,created_at FROM gate_events WHERE request_id=(SELECT group_id FROM gate_requests WHERE id=$1) ORDER BY id`, id))
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
	var id int64
	if err = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('gate_requests','id'))`).Scan(&id); err != nil {
		return zero, mapErr(err)
	}
	if _, err = s.createRow(ctx, tx, person, in, id, id); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, mapErr(err)
	}
	return s.get(ctx, person, id)
}
func (s *Gate) createRow(ctx context.Context, tx pgx.Tx, person int64, in model.GateInput, id, groupID int64) (int64, error) {
	var err error
	// Общая блокировка помещения сериализует проверку повторных заявок.
	var premise int64
	if err := tx.QueryRow(ctx, `SELECT p.id FROM premises p WHERE p.id=$2 AND `+residentOwns+` FOR UPDATE OF p`, person, in.PremisesID).Scan(&premise); err != nil {
		if err == pgx.ErrNoRows {
			return 0, invalid("gate: property is not eligible")
		}
		return 0, mapErr(err)
	}
	var ownership int64
	err = tx.QueryRow(ctx, `SELECT o.id FROM ownerships o JOIN premises p ON p.id=o.premises_id
 JOIN persons pe ON pe.id=o.person_id JOIN buildings b ON b.id=p.building_id JOIN organizations org ON org.id=b.organization_id
 WHERE o.person_id=$1 AND p.id=$2 AND o.deleted_at IS NULL AND pe.deleted_at IS NULL AND p.deleted_at IS NULL
 AND b.deleted_at IS NULL AND org.deleted_at IS NULL AND o.valid_from<=`+gateToday+` AND (o.valid_to IS NULL OR o.valid_to>=`+gateToday+`)
 AND ((p.kind IN('apartment','commercial') AND $3='courtyard') OR (p.kind='parking_space' AND $3='parking')) ORDER BY o.id LIMIT 1 FOR SHARE OF o`, person, in.PremisesID, in.Zone).Scan(&ownership)
	if err == pgx.ErrNoRows {
		return 0, invalid("gate: property is not eligible")
	}
	if err != nil {
		return 0, mapErr(err)
	}
	var valid bool
	if err := tx.QueryRow(ctx, `SELECT $1::date IS NULL OR $1::date>=`+gateToday, in.ValidUntil).Scan(&valid); err != nil {
		return 0, mapErr(err)
	}
	if !valid {
		return 0, invalid("gate: expiry is in the past")
	}
	if err := tx.QueryRow(ctx, gateView+`SELECT EXISTS(SELECT 1 FROM gate_view WHERE person_id=$1 AND premises_id=$2 AND zone=$3 AND phone=$4 AND effective_status IN('pending','approved'))`, person, in.PremisesID, in.Zone, in.Phone).Scan(&valid); err != nil {
		return 0, mapErr(err)
	}
	if valid {
		return 0, &Error{ErrConflict, "gate: duplicate request"}
	}
	err = tx.QueryRow(ctx, `INSERT INTO gate_requests(id,group_id,no_personal_vehicle,person_id,ownership_id,premises_id,zone,phone,recipient_name,relation,vehicle_plate,vehicle_brand,occasional_vehicle,valid_until,comment,vehicles)
 VALUES($13,$14,$15,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$16) RETURNING id`, person, ownership, in.PremisesID, in.Zone, in.Phone, in.RecipientName, in.Relation, in.VehiclePlate, in.VehicleBrand, in.OccasionalVehicle, in.ValidUntil, in.Comment, id, groupID, in.NoPersonalVehicle, in.StoredVehicles()).Scan(&id)
	if err != nil {
		return 0, mapErr(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO gate_events(request_id,action,actor_role,actor_person_id) VALUES($1,'submitted','resident',$2)`, id, person); err != nil {
		return 0, mapErr(err)
	}
	return id, nil
}
func (s *Gate) CreateBatch(ctx context.Context, person int64, in model.GateBatchInput) ([]model.GateRequest, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	ids := slices.Clone(in.PremisesIDs)
	slices.Sort(ids)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rollback(ctx, tx)
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM(SELECT p.id FROM premises p WHERE p.id=ANY($2::bigint[]) AND `+residentOwns+` ORDER BY p.id FOR UPDATE OF p) locked`, person, ids).Scan(&count)
	if err != nil {
		return nil, mapErr(err)
	}
	if count != len(ids) {
		return nil, invalid("gate: property is not eligible")
	}
	var groupID int64
	for _, premise := range ids {
		contact := in.Contact()
		contact.PremisesID = premise
		if err = tx.QueryRow(ctx, `SELECT CASE WHEN kind IN('apartment','commercial') THEN 'courtyard' WHEN kind='parking_space' THEN 'parking' ELSE '' END FROM premises WHERE id=$1`, premise).Scan(&contact.Zone); err != nil {
			return nil, mapErr(err)
		}
		if contact.Zone == "" {
			return nil, invalid("gate: property is not eligible")
		}
		contact.Normalize()
		var id int64
		if err = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('gate_requests','id'))`).Scan(&id); err != nil {
			return nil, mapErr(err)
		}
		if groupID == 0 {
			groupID = id
		}
		if _, err = s.createRow(ctx, tx, person, contact, id, groupID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, mapErr(err)
	}
	return collect[model.GateRequest](s.pool.Query(ctx, gateView+`SELECT * FROM gate_view WHERE group_id=$1 ORDER BY id`, groupID))
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
	var groupID int64
	err = tx.QueryRow(ctx, `SELECT group_id FROM gate_requests WHERE id=$1 AND ($2::bigint=0 OR person_id=$2)`, id, person).Scan(&groupID)
	if err != nil {
		return zero, mapErr(err)
	}
	type state struct {
		ID          int64  `db:"id"`
		Status      string `db:"status"`
		OwnershipID int64  `db:"ownership_id"`
	}
	states, err := collect[state](tx.Query(ctx, `SELECT id,status,ownership_id FROM gate_requests WHERE group_id=$1 ORDER BY id FOR UPDATE`, groupID))
	if err != nil {
		return zero, err
	}
	for _, row := range states {
		if (action == "revoke" && row.Status != "approved") || (action != "revoke" && row.Status != "pending") {
			return zero, &Error{ErrConflict, "gate: request status changed"}
		}
	}
	if action == "approve" {
		if _, err = tx.Exec(ctx, `SELECT id FROM ownerships WHERE id IN(SELECT ownership_id FROM gate_requests WHERE group_id=$1) ORDER BY id FOR SHARE`, groupID); err != nil {
			return zero, mapErr(err)
		}
		var eligible bool
		if err = tx.QueryRow(ctx, gateView+`SELECT COALESCE(bool_and(eligible AND (valid_until IS NULL OR valid_until>=`+gateToday+`)),false) FROM gate_view WHERE group_id=$1`, groupID).Scan(&eligible); err != nil {
			return zero, mapErr(err)
		}
		if !eligible {
			return zero, invalid("gate: property or expiry is no longer eligible")
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE gate_requests SET status=$2,review_note=$3,updated_at=now() WHERE group_id=$1`, groupID, target, note); err != nil {
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
	if _, err = tx.Exec(ctx, `INSERT INTO gate_events(request_id,action,actor_role,actor_person_id,actor_user_id,note) SELECT id,$2,$3,$4,$5,$6 FROM gate_requests WHERE group_id=$1`, groupID, target, role, actorPerson, actorUser, note); err != nil {
		return zero, mapErr(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, mapErr(err)
	}
	return s.get(ctx, person, id)
}
