package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"dom-backend/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Residents struct{ pool *pgxpool.Pool }

func NewResidents(pool *pgxpool.Pool) *Residents { return &Residents{pool} }
func ResidentTokenHash(raw string) string {
	v := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(v[:])
}
func residentToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}

// Любое раскрытие объекта проверяет действующее владение, включая родителей.
const residentMeterActive = `(m.installed_at <= (now() AT TIME ZONE 'Europe/Moscow')::date AND (m.removed_at IS NULL OR m.removed_at >= (now() AT TIME ZONE 'Europe/Moscow')::date))`

const residentOwns = `EXISTS(SELECT 1 FROM ownerships o JOIN persons pe ON pe.id=o.person_id
 JOIN premises rp ON rp.id=o.premises_id JOIN buildings rb ON rb.id=rp.building_id
 JOIN organizations ro ON ro.id=rb.organization_id
 WHERE o.person_id=$1 AND o.premises_id=p.id AND o.deleted_at IS NULL AND pe.deleted_at IS NULL
 AND rp.deleted_at IS NULL AND rb.deleted_at IS NULL AND ro.deleted_at IS NULL
 AND o.valid_from <= (now() AT TIME ZONE 'Europe/Moscow')::date
 AND (o.valid_to IS NULL OR o.valid_to >= (now() AT TIME ZONE 'Europe/Moscow')::date))`
const residentEnabled = `a.active AND ra.enabled AND a.email=ra.email AND pe.deleted_at IS NULL AND EXISTS (
 SELECT 1 FROM ownerships o JOIN premises p ON p.id=o.premises_id JOIN buildings b ON b.id=p.building_id
 JOIN organizations org ON org.id=b.organization_id WHERE o.person_id=a.person_id AND o.deleted_at IS NULL
 AND p.deleted_at IS NULL AND b.deleted_at IS NULL AND org.deleted_at IS NULL
 AND o.valid_from <= (now() AT TIME ZONE 'Europe/Moscow')::date
 AND (o.valid_to IS NULL OR o.valid_to >= (now() AT TIME ZONE 'Europe/Moscow')::date))`

func (s *Residents) ByEmail(ctx context.Context, email string) (model.ResidentAccount, error) {
	return one[model.ResidentAccount](s.pool.Query(ctx, `SELECT a.id,a.person_id,a.email,a.password_hash FROM resident_accounts a JOIN resident_access ra ON ra.person_id=a.person_id JOIN persons pe ON pe.id=a.person_id WHERE a.email=$1 AND `+residentEnabled, email))
}
func (s *Residents) Session(ctx context.Context, raw string) (model.ResidentAccount, error) {
	return one[model.ResidentAccount](s.pool.Query(ctx, `SELECT a.id,a.person_id,a.email,a.password_hash FROM resident_sessions rs JOIN resident_accounts a ON a.id=rs.account_id JOIN resident_access ra ON ra.person_id=a.person_id JOIN persons pe ON pe.id=a.person_id WHERE rs.token_hash=$1 AND rs.expires_at>now() AND `+residentEnabled, ResidentTokenHash(raw)))
}
func (s *Residents) NewSession(ctx context.Context, id int64, passwordHash string) (string, error) {
	raw, err := residentToken()
	if err != nil {
		return "", err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer rollback(ctx, tx)
	var person int64
	if err = tx.QueryRow(ctx, `SELECT person_id FROM resident_accounts WHERE id=$1`, id).Scan(&person); err != nil {
		return "", mapErr(err)
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('resident:' || $1::bigint::text,0))`, person); err != nil {
		return "", err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO resident_sessions(token_hash,account_id,expires_at) SELECT $1,a.id,now()+interval '7 days' FROM resident_accounts a JOIN resident_access ra ON ra.person_id=a.person_id JOIN persons pe ON pe.id=a.person_id WHERE a.id=$2 AND a.password_hash=$3 AND `+residentEnabled, ResidentTokenHash(raw), id, passwordHash)
	if err != nil {
		return "", mapErr(err)
	}
	if tag.RowsAffected() != 1 {
		return "", ErrNotFound
	}
	if _, err = tx.Exec(ctx, `UPDATE resident_accounts SET last_login_at=now() WHERE id=$1`, id); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return raw, nil
}

func (s *Residents) Logout(ctx context.Context, raw string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM resident_sessions WHERE token_hash=$1`, ResidentTokenHash(raw))
	return err
}

// RequestToken всегда возвращает одинаковый результат, в том числе для неизвестного email.
func (s *Residents) RequestToken(ctx context.Context, email, purpose, baseURL string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var person int64
	err = tx.QueryRow(ctx, `SELECT ra.person_id FROM resident_access ra JOIN persons pe ON pe.id=ra.person_id
 WHERE ra.enabled AND ra.email=$1 AND pe.deleted_at IS NULL
 AND EXISTS(SELECT 1 FROM ownerships o JOIN premises p ON p.id=o.premises_id JOIN buildings b ON b.id=p.building_id JOIN organizations org ON org.id=b.organization_id WHERE o.person_id=ra.person_id AND o.deleted_at IS NULL AND p.deleted_at IS NULL AND b.deleted_at IS NULL AND org.deleted_at IS NULL AND o.valid_from<=(now() AT TIME ZONE 'Europe/Moscow')::date AND (o.valid_to IS NULL OR o.valid_to>=(now() AT TIME ZONE 'Europe/Moscow')::date))
 AND (($2='activate' AND NOT EXISTS(SELECT 1 FROM resident_accounts a WHERE a.person_id=ra.person_id))
 OR ($2='reset' AND EXISTS(SELECT 1 FROM resident_accounts a WHERE a.person_id=ra.person_id AND a.active AND a.email=$1)))`, email, purpose).Scan(&person)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	raw, err := residentToken()
	if err != nil {
		return err
	}
	// Не отзываем прежнюю ссылку по публичному запросу: это позволило бы мешать активации.
	_, err = tx.Exec(ctx, `INSERT INTO resident_tokens(token_hash,person_id,email,purpose,expires_at) VALUES($1,$2,$3,$4,now()+interval '48 hours')`, ResidentTokenHash(raw), person, email, purpose)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO resident_mail_outbox(email,link,expires_at) VALUES($1,$2,now()+interval '48 hours')`, email, baseURL+"/#set-password?token="+raw)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Residents) RedeemToken(ctx context.Context, raw, hash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var person int64
	var email, purpose string
	err = tx.QueryRow(ctx, `SELECT person_id,email,purpose FROM resident_tokens WHERE token_hash=$1 AND expires_at>now()`, ResidentTokenHash(raw)).Scan(&person, &email, &purpose)
	if err != nil {
		return invalid("invalid or expired link")
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('resident:' || $1::bigint::text,0))`, person); err != nil {
		return err
	}
	var tokenPresent bool
	if err = tx.QueryRow(ctx, `SELECT true FROM resident_tokens WHERE token_hash=$1 AND expires_at>now() FOR UPDATE`, ResidentTokenHash(raw)).Scan(&tokenPresent); err != nil {
		return invalid("invalid or expired link")
	}
	// Блокировка подтверждения сериализует конкурирующие активации и отключение доступа.
	var approved bool
	err = tx.QueryRow(ctx, `SELECT enabled AND email=$2 AND EXISTS(SELECT 1 FROM premises p WHERE `+residentOwns+`) FROM resident_access WHERE person_id=$1 FOR UPDATE`, person, email).Scan(&approved)
	if err != nil || !approved {
		return invalid("invalid or expired link")
	}
	if purpose == "activate" {
		_, err = tx.Exec(ctx, `INSERT INTO resident_accounts(person_id,email,password_hash) VALUES($1,$2,$3)`, person, email, hash)
	} else {
		tag, e := tx.Exec(ctx, `UPDATE resident_accounts SET password_hash=$3 WHERE person_id=$1 AND email=$2 AND active`, person, email, hash)
		err = e
		if err == nil && tag.RowsAffected() != 1 {
			return invalid("invalid or expired link")
		}
	}
	if err != nil {
		return mapErr(err)
	}
	_, err = tx.Exec(ctx, `DELETE FROM resident_sessions WHERE account_id IN(SELECT id FROM resident_accounts WHERE person_id=$1)`, person)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM resident_tokens WHERE person_id=$1`, person)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM resident_mail_outbox WHERE email=$1`, email); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Residents) ChangePassword(ctx context.Context, id int64, oldHash, newHash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var person int64
	if err = tx.QueryRow(ctx, `SELECT person_id FROM resident_accounts WHERE id=$1`, id).Scan(&person); err != nil {
		return mapErr(err)
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('resident:' || $1::bigint::text,0))`, person); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE resident_accounts SET password_hash=$3 WHERE id=$1 AND password_hash=$2 AND active`, id, oldHash, newHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return invalid("password changed; sign in again")
	}
	_, err = tx.Exec(ctx, `DELETE FROM resident_sessions WHERE account_id=$1`, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM resident_tokens WHERE person_id=(SELECT person_id FROM resident_accounts WHERE id=$1)`, id)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM resident_mail_outbox WHERE email=(SELECT email FROM resident_accounts WHERE id=$1)`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Residents) Profile(ctx context.Context, person int64) (model.Person, error) {
	return NewPersons(s.pool).Get(ctx, person)
}
func (s *Residents) Properties(ctx context.Context, person int64) ([]model.ResidentProperty, error) {
	return collect[model.ResidentProperty](s.pool.Query(ctx, `SELECT p.id,p.building_id,p.kind,p.number,p.entrance,p.floor,p.total_area,p.living_area,p.rooms,p.cadastral_number,p.created_at,p.updated_at,p.deleted_at,b.address,k.name AS kind_name,o.share_num,o.share_den
 FROM premises p JOIN buildings b ON b.id=p.building_id JOIN premises_kinds k ON k.code=p.kind
 JOIN ownerships o ON o.premises_id=p.id AND o.person_id=$1 AND o.deleted_at IS NULL
 AND o.valid_from<=(now() AT TIME ZONE 'Europe/Moscow')::date AND (o.valid_to IS NULL OR o.valid_to>=(now() AT TIME ZONE 'Europe/Moscow')::date)
 WHERE `+residentOwns+` ORDER BY p.building_id,p.kind,substring(p.number from '\d+')::numeric NULLS LAST,p.number`, person))
}
func (s *Residents) Meters(ctx context.Context, person int64) ([]model.Meter, error) {
	// Служебные комментарии счётчика не входят в ответ кабинета.
	meters, err := collect[model.Meter](s.pool.Query(ctx, strings.ReplaceAll(meterSelect, "CURRENT_DATE", "(now() AT TIME ZONE 'Europe/Moscow')::date")+` WHERE m.deleted_at IS NULL AND `+residentOwns+` ORDER BY p.id,array_position(ARRAY['cold_water','hot_water','electricity'],m.resource),m.id`, person))
	for i := range meters {
		meters[i].Comment = nil
	}
	return meters, err
}
func (s *Residents) Windows(ctx context.Context, person int64) ([]model.ResidentWindow, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT p.building_id FROM premises p WHERE `+residentOwns+` ORDER BY p.building_id`, person)
	if err != nil {
		return nil, mapErr(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, mapErr(err)
	}
	settings, err := loadResidentSettings(ctx, s.pool, ids)
	if err != nil {
		return nil, err
	}
	out := []model.ResidentWindow{}
	for _, id := range ids {
		if v, ok := settings[id]; ok {
			out = append(out, v.CurrentWindow)
		}
	}
	return out, nil
}

func (s *Residents) Readings(ctx context.Context, a model.ResidentAccount, meter int64, limit, offset int) ([]model.ResidentReading, error) {
	var building int64
	err := s.pool.QueryRow(ctx, `SELECT p.building_id FROM meters m JOIN premises p ON p.id=m.premises_id WHERE m.id=$2 AND m.deleted_at IS NULL AND `+residentOwns, a.PersonID, meter).Scan(&building)
	if err != nil {
		return nil, mapErr(err)
	}
	settings, err := s.Schedule(ctx, building)
	if err != nil {
		return nil, err
	}
	w := settings.CurrentWindow
	q := `SELECT rr.*,r.resident_account_id AS author_id,COALESCE((SELECT max(id) FROM resident_reading_audit WHERE reading_id=r.id),0) AS revision,
 COALESCE(r.source='resident' AND r.resident_account_id=$3 AND r.period=$6 AND $7::bool AND ` + residentMeterActive + `,false) AS editable
 FROM (` + readingSelect + `) rr JOIN meter_readings r ON r.id=rr.id JOIN meters m ON m.id=r.meter_id JOIN premises p ON p.id=m.premises_id
 WHERE r.meter_id=$2 AND r.deleted_at IS NULL AND ` + residentOwns + ` ORDER BY r.reading_date DESC,r.id DESC LIMIT $4 OFFSET $5`
	return collect[model.ResidentReading](s.pool.Query(ctx, q, a.PersonID, meter, a.ID, limit, offset, w.Period, w.IsOpen(w.ServerDate)))
}

func (s *Residents) SubmitReading(ctx context.Context, a model.ResidentAccount, in model.MeterReading, expected *int64) (model.MeterReading, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.MeterReading{}, err
	}
	defer rollback(ctx, tx)
	id, err := writeResidentReading(ctx, tx, a, in, expected, nil, nil)
	if err != nil {
		return model.MeterReading{}, err
	}
	result, err := one[model.MeterReading](tx.Query(ctx, readingSelect+` WHERE r.id=$1`, id))
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.MeterReading{}, mapErr(err)
	}
	return result, nil
}
func writeResidentReading(ctx context.Context, tx pgx.Tx, a model.ResidentAccount, in model.MeterReading, expected *int64, premises *int64, shared *model.ResidentWindow) (int64, error) {
	// Сначала счётчик, как в административном хранилище: единый порядок блокировок.
	var meter, building int64
	err := tx.QueryRow(ctx, `SELECT m.id,p.building_id FROM meters m JOIN premises p ON p.id=m.premises_id WHERE m.id=$2 AND ($3::bigint IS NULL OR p.id=$3) AND m.deleted_at IS NULL AND `+residentMeterActive+` AND `+residentOwns+` FOR UPDATE OF m`, a.PersonID, in.MeterID, premises).Scan(&meter, &building)
	if err != nil {
		return 0, mapErr(err)
	}
	var window model.ResidentWindow
	if shared != nil {
		window = *shared
	} else {
		window, err = lockResidentWindow(ctx, tx, building)
		if err != nil {
			return 0, err
		}
	}
	if !in.Period.Equal(window.Period.Time) {
		return 0, invalid("period: only the open period is allowed")
	}
	in.ReadingDate = window.ServerDate
	in.Source = "resident"
	var id int64
	var author *int64
	var source string
	var before []byte
	var version int64
	err = tx.QueryRow(ctx, `SELECT id,resident_account_id,source,to_jsonb(r),COALESCE((SELECT max(id) FROM resident_reading_audit WHERE reading_id=r.id),0) FROM meter_readings r WHERE meter_id=$1 AND period=$2 AND deleted_at IS NULL FOR UPDATE`, meter, in.Period).Scan(&id, &author, &source, &before, &version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if id != 0 && (source != "resident" || author == nil || *author != a.ID) {
		return 0, &Error{ErrConflict, "reading already submitted; contact management"}
	}
	if id != 0 && (expected == nil || *expected != version) {
		return 0, &Error{ErrConflict, "reading changed; refresh and try again"}
	}
	if id == 0 && expected != nil {
		return 0, &Error{ErrConflict, "reading changed; refresh and try again"}
	}
	if err = checkReading(ctx, tx, in, id); err != nil {
		return 0, err
	}
	if id == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO meter_readings(meter_id,reading_date,period,t1,t2,t3,source,comment,resident_account_id) VALUES($1,$2,$3,$4,$5,$6,'resident',$7,$8) RETURNING id`, meter, in.ReadingDate, in.Period, in.T1, in.T2, in.T3, in.Comment, a.ID).Scan(&id)
	} else {
		_, err = tx.Exec(ctx, `UPDATE meter_readings SET reading_date=$2,t1=$3,t2=$4,t3=$5,comment=$6,updated_at=now() WHERE id=$1`, id, in.ReadingDate, in.T1, in.T2, in.T3, in.Comment)
	}
	if err != nil {
		return 0, mapErr(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO resident_reading_audit(reading_id,account_id,before_values,after_values) SELECT id,$2,$3::jsonb,to_jsonb(r) FROM meter_readings r WHERE id=$1`, id, a.ID, json.RawMessage(before))
	if err != nil {
		return 0, err
	}

	return id, nil
}

// Audit не раскрывает идентификаторы/профили совладельцев и административные поля.
func (s *Residents) Audit(ctx context.Context, a model.ResidentAccount, meter int64) ([]json.RawMessage, error) {
	_, err := s.Readings(ctx, a, meter, 1, 0)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT jsonb_build_object('id',h.id,'created_at',h.created_at,'mine',h.account_id=$2,'before',CASE WHEN h.before_values IS NULL THEN NULL ELSE jsonb_build_object('t1',h.before_values->'t1','t2',h.before_values->'t2','t3',h.before_values->'t3','comment',h.before_values->'comment') END,'after',jsonb_build_object('period',h.after_values->'period','t1',h.after_values->'t1','t2',h.after_values->'t2','t3',h.after_values->'t3','comment',h.after_values->'comment')) FROM resident_reading_audit h JOIN meter_readings r ON r.id=h.reading_id WHERE r.meter_id=$1 AND r.deleted_at IS NULL ORDER BY h.id DESC LIMIT 100`, meter, a.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(b))
	}
	return out, rows.Err()
}

func (s *Residents) Approve(ctx context.Context, person, admin int64, email string, enabled bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('resident:' || $1::bigint::text,0))`, person); err != nil {
		return err
	}
	var profileHasEmail bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM persons p, unnest(p.emails) e WHERE p.id=$1 AND p.deleted_at IS NULL AND lower(e)=lower($2))`, person, email).Scan(&profileHasEmail); err != nil {
		return err
	}
	if !profileHasEmail {
		return invalid("resident email must be listed in the person's profile")
	}
	var existing string
	lookup := tx.QueryRow(ctx, `SELECT email FROM resident_accounts WHERE person_id=$1 FOR UPDATE`, person).Scan(&existing)
	if lookup != nil && !errors.Is(lookup, pgx.ErrNoRows) {
		return lookup
	}
	if lookup == nil && existing != email {
		return invalid("account email cannot be changed through access approval")
	}
	tag, err := tx.Exec(ctx, `INSERT INTO resident_access(person_id,email,enabled,approved_by) SELECT id,$2,$3,$4 FROM persons WHERE id=$1 AND deleted_at IS NULL ON CONFLICT(person_id) DO UPDATE SET email=excluded.email,enabled=excluded.enabled,approved_by=excluded.approved_by,updated_at=now()`, person, email, enabled, admin)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	_, err = tx.Exec(ctx, `DELETE FROM resident_sessions WHERE account_id IN(SELECT id FROM resident_accounts WHERE person_id=$1)`, person)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM resident_tokens WHERE person_id=$1`, person)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM resident_mail_outbox WHERE email=$1`, email); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FlushMail отправляет не более одного письма; блокировка защищает от второго worker.
func (s *Residents) FlushMail(ctx context.Context, send func(context.Context, string, string, string, string) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	_, err = tx.Exec(ctx, `DELETE FROM resident_mail_outbox WHERE expires_at<=now(); DELETE FROM resident_tokens WHERE expires_at<=now(); DELETE FROM resident_sessions WHERE expires_at<=now()`)
	if err != nil {
		return err
	}
	var id int64
	var email, link, firstName, middleName string
	err = tx.QueryRow(ctx, `SELECT o.id,o.email,o.link,COALESCE(p.first_name,''),COALESCE(p.middle_name,'') FROM resident_mail_outbox o
 LEFT JOIN resident_access ra ON ra.email=o.email LEFT JOIN persons p ON p.id=ra.person_id
 WHERE o.next_attempt_at<=now() ORDER BY o.id LIMIT 1 FOR UPDATE OF o SKIP LOCKED`).Scan(&id, &email, &link, &firstName, &middleName)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if err = send(ctx, email, link, firstName, middleName); err != nil {
		_, e := tx.Exec(ctx, `UPDATE resident_mail_outbox SET attempts=attempts+1,next_attempt_at=now()+interval '1 minute' WHERE id=$1`, id)
		if e != nil {
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		return fmt.Errorf("resident mail outbox id=%d delivery failed: %w", id, err)
	}
	_, err = tx.Exec(ctx, `DELETE FROM resident_mail_outbox WHERE id=$1`, id)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const residentAccessSelect = `SELECT p.id AS person_id,ra.email,ra.person_id IS NOT NULL AS configured,
 COALESCE(ra.enabled,false) AS enabled,a.id IS NOT NULL AS registered,a.created_at AS registered_at,a.last_login_at,
 COALESCE(a.active,false) AS account_active,(p.deleted_at IS NULL AND EXISTS(
 SELECT 1 FROM ownerships o JOIN premises rp ON rp.id=o.premises_id
 JOIN buildings rb ON rb.id=rp.building_id JOIN organizations ro ON ro.id=rb.organization_id
 WHERE o.person_id=p.id AND o.deleted_at IS NULL AND rp.deleted_at IS NULL
 AND rb.deleted_at IS NULL AND ro.deleted_at IS NULL
 AND o.valid_from<=(now() AT TIME ZONE 'Europe/Moscow')::date
 AND (o.valid_to IS NULL OR o.valid_to>=(now() AT TIME ZONE 'Europe/Moscow')::date))) AS eligible
 FROM persons p LEFT JOIN resident_access ra ON ra.person_id=p.id LEFT JOIN resident_accounts a ON a.person_id=p.id`

func (s *Residents) ListAccess(ctx context.Context, ids []int64) ([]model.ResidentAccess, error) {
	return collect[model.ResidentAccess](s.pool.Query(ctx, residentAccessSelect+` WHERE p.id=ANY($1::bigint[]) ORDER BY p.id`, ids))
}
func (s *Residents) Access(ctx context.Context, person int64) (map[string]any, error) {
	states, err := s.ListAccess(ctx, []int64{person})
	if err != nil {
		return nil, err
	}
	if len(states) != 1 {
		return nil, ErrNotFound
	}
	v := states[0]
	return map[string]any{"person_id": v.PersonID, "email": v.Email, "configured": v.Configured, "enabled": v.Enabled, "registered": v.Registered, "registered_at": v.RegisteredAt, "last_login_at": v.LastLoginAt, "account_active": v.AccountActive, "eligible": v.Eligible}, nil
}
