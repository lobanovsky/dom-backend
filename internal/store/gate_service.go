package store

import (
	"context"
	"sort"

	"dom-backend/internal/model"
	"github.com/jackc/pgx/v5"
)

func (s *Gate) ServiceAccesses(ctx context.Context) ([]model.GateServiceAccess, error) {
	items, err := collect[model.GateServiceAccess](s.pool.Query(ctx, `SELECT id,category,name,phone,vehicle_plate,vehicle_brand,valid_until,comment,created_at FROM gate_service_accesses ORDER BY id`))
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Zones, err = collect[model.GateServiceZone](s.pool.Query(ctx, `SELECT zone,status,sync_error FROM gate_service_access_zones WHERE access_id=$1 ORDER BY zone`, items[i].ID))
		if err != nil {
			return nil, err
		}
	}
	// ELDES держит один номер телефона на зону. Объединяем старые отдельные
	// записи с тем же телефоном в одну строку на каждую зону.
	merged := make([]model.GateServiceAccess, 0, len(items))
	indexByPhone := make(map[string]int, len(items))
	for _, item := range items {
		index, exists := indexByPhone[item.Phone]
		if !exists {
			indexByPhone[item.Phone] = len(merged)
			merged = append(merged, item)
			continue
		}
		current := &merged[index]
		for _, zone := range item.Zones {
			found := false
			for i := range current.Zones {
				if current.Zones[i].Zone != zone.Zone {
					continue
				}
				found = true
				if gateServiceStatusPriority(zone.Status) > gateServiceStatusPriority(current.Zones[i].Status) || (current.Zones[i].SyncError == "" && zone.SyncError != "") {
					current.Zones[i] = zone
				}
				break
			}
			if !found {
				current.Zones = append(current.Zones, zone)
			}
		}
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].CreatedAt.After(merged[j].CreatedAt) })
	return merged, nil
}

func gateServiceStatusPriority(status string) int {
	switch status {
	case "active":
		return 4
	case "revoke_pending":
		return 3
	case "expired":
		return 2
	default:
		return 1
	}
}

func (s *Gate) LookupGatePhone(ctx context.Context, phone string) ([]model.GatePhoneLookupEntry, error) {
	return collect[model.GatePhoneLookupEntry](s.pool.Query(ctx, gateView+`SELECT zone,'property' source,recipient_name name,
CASE premises_kind WHEN 'parking_space' THEN 'Машиноместо ' ELSE CASE WHEN premises_kind='commercial' THEN 'Коммерческое помещение ' ELSE 'Квартира ' END END || premises_number || ', ' || address location,
effective_status status,(active OR status='revoke_requested') on_device,''::text sync_error
FROM gate_view WHERE phone=$1 AND (active OR status='revoke_requested' OR effective_status='pending')
UNION ALL
SELECT z.zone,'service' source,(array_agg(a.name ORDER BY a.id))[1] name,'' location,
CASE WHEN bool_or(z.status='active') THEN 'active' ELSE 'revoke_pending' END status,
true on_device,COALESCE(max(NULLIF(z.sync_error,'')),'') sync_error
FROM gate_service_access_zones z JOIN gate_service_accesses a ON a.id=z.access_id
WHERE a.phone=$1 AND z.status IN('active','revoke_pending') AND (z.status<>'active' OR a.valid_until IS NULL OR a.valid_until>=`+gateToday+`)
GROUP BY z.zone
ORDER BY zone,source,location,name`, phone))
}

func (s *Gate) CreateServiceAccess(ctx context.Context, user model.User, in model.GateServiceAccessInput) (model.GateServiceAccess, error) {
	var zero model.GateServiceAccess
	if !user.IsAdmin() {
		return zero, invalid("forbidden")
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return zero, err
	}
	if s.syncer == nil {
		return zero, &Error{ErrIntegration, "интеграция ELDES не настроена"}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, mapErr(err)
	}
	defer rollback(ctx, tx)
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT $1::date IS NULL OR $1::date>=`+gateToday, in.ValidUntil).Scan(&valid); err != nil {
		return zero, mapErr(err)
	}
	if !valid {
		return zero, invalid("gate: expiry is in the past")
	}
	keys := append([]string{"service-access:" + in.Phone}, in.Zones...)
	for i := range keys {
		keys[i] += ":" + in.Phone
	}
	slicesSort(keys)
	for _, key := range keys {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
			return zero, mapErr(err)
		}
	}
	var id int64
	err = tx.QueryRow(ctx, `SELECT id FROM gate_service_accesses WHERE phone=$1 ORDER BY id LIMIT 1 FOR UPDATE`, in.Phone).Scan(&id)
	if err == pgx.ErrNoRows {
		err = tx.QueryRow(ctx, `INSERT INTO gate_service_accesses(category,name,phone,vehicle_plate,vehicle_brand,valid_until,comment,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, in.Category, in.Name, in.Phone, in.VehiclePlate, in.VehicleBrand, in.ValidUntil, in.Comment, user.ID).Scan(&id)
		if err != nil {
			return zero, mapErr(err)
		}
	} else if err != nil {
		return zero, mapErr(err)
	} else {
		if _, err = tx.Exec(ctx, `UPDATE gate_service_accesses SET category=$2,name=$3,vehicle_plate=$4,vehicle_brand=$5,valid_until=$6,comment=$7,updated_at=now() WHERE phone=$1`, in.Phone, in.Category, in.Name, in.VehiclePlate, in.VehicleBrand, in.ValidUntil, in.Comment); err != nil {
			return zero, mapErr(err)
		}
	}
	var needSync []string
	for _, zone := range in.Zones {
		var exists bool
		if err = tx.QueryRow(ctx, gateView+`SELECT EXISTS(SELECT 1 FROM gate_view WHERE zone=$1 AND phone=$2 AND active) OR EXISTS(SELECT 1 FROM gate_service_access_zones z JOIN gate_service_accesses a ON a.id=z.access_id WHERE z.zone=$1 AND a.phone=$2 AND z.status='active' AND (a.valid_until IS NULL OR a.valid_until>=`+gateToday+`))`, zone, in.Phone).Scan(&exists); err != nil {
			return zero, mapErr(err)
		}
		if !exists {
			needSync = append(needSync, zone)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO gate_service_access_zones(access_id,zone) VALUES($1,$2) ON CONFLICT(access_id,zone) DO UPDATE SET status='active',pending_action='',sync_error='',updated_at=now()`, id, zone); err != nil {
			return zero, mapErr(err)
		}
		action, note := "zone_added", "Служебная зона добавлена"
		if exists {
			action, note = "duplicate_ignored", "Телефон уже имел доступ в этой зоне; повторная запись объединена"
		}
		if _, err = tx.Exec(ctx, `INSERT INTO gate_service_access_events(access_id,zone,action,actor_user_id,note) VALUES($1,$2,$3,$4,$5)`, id, zone, action, user.ID, note); err != nil {
			return zero, mapErr(err)
		}
	}
	var synced []string
	for _, zone := range needSync {
		if err = s.syncer.Sync(ctx, zone, in.Phone, gateDeviceName(in.Name), true); err != nil {
			for i := len(synced) - 1; i >= 0; i-- {
				_ = s.syncer.Sync(ctx, synced[i], in.Phone, gateDeviceName(in.Name), false)
			}
			return zero, &Error{ErrIntegration, "не удалось добавить доступ на устройствах"}
		}
		synced = append(synced, zone)
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, mapErr(err)
	}
	all, err := s.ServiceAccesses(ctx)
	if err != nil {
		return zero, err
	}
	for _, item := range all {
		if item.Phone == in.Phone {
			return item, nil
		}
	}
	return zero, &Error{ErrNotFound, "not found"}
}

func slicesSort(v []string) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

func (s *Gate) RevokeServiceZone(ctx context.Context, user model.User, id int64, zone string) error {
	if !user.IsAdmin() || (zone != "courtyard" && zone != "parking") {
		return invalid("invalid access zone")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return mapErr(err)
	}
	defer rollback(ctx, tx)
	var phone, name string
	err = tx.QueryRow(ctx, `SELECT phone,name FROM gate_service_accesses WHERE id=$1`, id).Scan(&phone, &name)
	if err != nil {
		return mapErr(err)
	}
	key := zone + ":" + phone
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
		return mapErr(err)
	}
	var active bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gate_service_access_zones z JOIN gate_service_accesses a ON a.id=z.access_id WHERE a.phone=$1 AND z.zone=$2 AND z.status='active' AND (a.valid_until IS NULL OR a.valid_until>=`+gateToday+`))`, phone, zone).Scan(&active)
	if err != nil {
		return mapErr(err)
	}
	if !active {
		return &Error{ErrConflict, "доступ уже закрыт или ожидает удаления"}
	}
	var remains bool
	err = tx.QueryRow(ctx, gateView+`SELECT EXISTS(SELECT 1 FROM gate_view WHERE zone=$1 AND phone=$2 AND active)`, zone, phone).Scan(&remains)
	if err != nil {
		return mapErr(err)
	}
	if !remains {
		if s.syncer == nil {
			return &Error{ErrIntegration, "интеграция ELDES не настроена"}
		}
		if err = s.syncer.Sync(ctx, zone, phone, gateDeviceName(name), false); err != nil {
			_, _ = tx.Exec(ctx, `WITH changed AS (UPDATE gate_service_access_zones z SET status='revoke_pending',pending_action='revoke',sync_error=$3,updated_at=now() FROM gate_service_accesses a WHERE z.access_id=a.id AND a.phone=$1 AND z.zone=$2 AND z.status='active' RETURNING z.access_id) INSERT INTO gate_service_access_events(access_id,zone,action,actor_user_id,note) SELECT access_id,$2,'revoke_pending',$4,$3 FROM changed`, phone, zone, err.Error(), user.ID)
			if e := tx.Commit(ctx); e != nil {
				return mapErr(e)
			}
			return &Error{ErrIntegration, "удаление из ELDES ожидает повтора"}
		}
	}
	_, err = tx.Exec(ctx, `WITH changed AS (UPDATE gate_service_access_zones z SET status='revoked',pending_action='',sync_error='',updated_at=now() FROM gate_service_accesses a WHERE z.access_id=a.id AND a.phone=$1 AND z.zone=$2 AND z.status IN ('active','revoke_pending') RETURNING z.access_id) INSERT INTO gate_service_access_events(access_id,zone,action,actor_user_id) SELECT access_id,$2,'revoked',$3 FROM changed`, phone, zone, user.ID)
	if err != nil {
		return mapErr(err)
	}
	return mapErr(tx.Commit(ctx))
}

// ProcessExpiredServiceAccesses closes expired grants and retries device removals that previously failed.
func (s *Gate) ProcessExpiredServiceAccesses(ctx context.Context) error {
	type item struct {
		ID            int64  `db:"id"`
		AccessID      int64  `db:"access_id"`
		Zone          string `db:"zone"`
		Phone         string `db:"phone"`
		Name          string `db:"name"`
		Status        string `db:"status"`
		PendingAction string `db:"pending_action"`
	}
	items, err := collect[item](s.pool.Query(ctx, `SELECT z.id,z.access_id,z.zone,a.phone,a.name,z.status,z.pending_action FROM gate_service_access_zones z JOIN gate_service_accesses a ON a.id=z.access_id WHERE z.status='revoke_pending' OR (z.status='active' AND a.valid_until IS NOT NULL AND a.valid_until<`+gateToday+`) ORDER BY z.id`))
	if err != nil {
		return err
	}
	for _, it := range items {
		tx, e := s.pool.Begin(ctx)
		if e != nil {
			return mapErr(e)
		}
		key := it.Zone + ":" + it.Phone
		_, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key)
		if e != nil {
			rollback(ctx, tx)
			return mapErr(e)
		}
		var other bool
		e = tx.QueryRow(ctx, gateView+`SELECT EXISTS(SELECT 1 FROM gate_view WHERE zone=$1 AND phone=$2 AND active) OR EXISTS(SELECT 1 FROM gate_service_access_zones z JOIN gate_service_accesses a ON a.id=z.access_id WHERE z.id<>$3 AND z.zone=$1 AND a.phone=$2 AND z.status='active' AND (a.valid_until IS NULL OR a.valid_until>=`+gateToday+`))`, it.Zone, it.Phone, it.ID).Scan(&other)
		if e != nil {
			rollback(ctx, tx)
			return mapErr(e)
		}
		if !other {
			if s.syncer == nil {
				e = &Error{ErrIntegration, "ELDES integration is not configured"}
			} else {
				e = s.syncer.Sync(ctx, it.Zone, it.Phone, gateDeviceName(it.Name), false)
			}
		}
		if e != nil {
			_, _ = tx.Exec(ctx, `UPDATE gate_service_access_zones SET status='revoke_pending',pending_action=CASE WHEN status='active' THEN 'expire' ELSE pending_action END,sync_error=$2,updated_at=now() WHERE id=$1`, it.ID, e.Error())
			if it.Status == "active" {
				_, _ = tx.Exec(ctx, `INSERT INTO gate_service_access_events(access_id,zone,action,note) VALUES($1,$2,'revoke_pending',$3)`, it.AccessID, it.Zone, e.Error())
			}
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return mapErr(commitErr)
			}
			continue
		}
		status := "expired"
		action := "expired"
		if it.Status == "revoke_pending" && it.PendingAction == "revoke" {
			status = "revoked"
			action = "revoked"
		}
		_, e = tx.Exec(ctx, `UPDATE gate_service_access_zones SET status=$2,pending_action='',sync_error='',updated_at=now() WHERE id=$1`, it.ID, status)
		if e == nil && (it.Status == "active" || it.Status == "revoke_pending") {
			_, e = tx.Exec(ctx, `INSERT INTO gate_service_access_events(access_id,zone,action,note) VALUES($1,$2,$3,'Срок доступа истёк')`, it.AccessID, it.Zone, action)
		}
		if e != nil {
			rollback(ctx, tx)
			return mapErr(e)
		}
		if e = tx.Commit(ctx); e != nil {
			return mapErr(e)
		}
	}
	return nil
}
