package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

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
	_, err = tx.Exec(ctx, `INSERT INTO resident_tokens(token_hash,person_id,email,purpose,expires_at) VALUES($1,$2,$3,$4,now()+interval '30 minutes')`, ResidentTokenHash(raw), person, email, purpose)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO resident_mail_outbox(email,link,expires_at) VALUES($1,$2,now()+interval '30 minutes')`, email, baseURL+"/#set-password?token="+raw)
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
	return collect[model.ResidentWindow](s.pool.Query(ctx, `SELECT w.building_id,w.period,w.opens_on,w.closes_on FROM resident_reading_windows w WHERE EXISTS(SELECT 1 FROM premises p WHERE p.building_id=w.building_id AND `+residentOwns+`)`, person))
}
func (s *Residents) Readings(ctx context.Context, a model.ResidentAccount, meter int64, limit, offset int) ([]model.ResidentReading, error) {
	var allowed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM meters m JOIN premises p ON p.id=m.premises_id WHERE m.id=$2 AND m.deleted_at IS NULL AND `+residentOwns+`)`, a.PersonID, meter).Scan(&allowed)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrNotFound
	}
	q := `SELECT rr.*, r.resident_account_id AS author_id,COALESCE((SELECT max(id) FROM resident_reading_audit WHERE reading_id=r.id),0) AS revision,COALESCE(r.source='resident' AND r.resident_account_id=$3 AND w.period=r.period AND (now() AT TIME ZONE 'Europe/Moscow')::date BETWEEN w.opens_on AND w.closes_on AND ` + residentMeterActive + `,false) AS editable FROM (` + readingSelect + `) rr JOIN meter_readings r ON r.id=rr.id JOIN meters m ON m.id=r.meter_id JOIN premises p ON p.id=m.premises_id LEFT JOIN resident_reading_windows w ON w.building_id=p.building_id WHERE r.meter_id=$2 AND r.deleted_at IS NULL AND ` + residentOwns + ` ORDER BY r.reading_date DESC,r.id DESC LIMIT $4 OFFSET $5`
	return collect[model.ResidentReading](s.pool.Query(ctx, q, a.PersonID, meter, a.ID, limit, offset))
}
func (s *Residents) SubmitReading(ctx context.Context, a model.ResidentAccount, in model.MeterReading, expected *int64) (model.MeterReading, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.MeterReading{}, err
	}
	defer rollback(ctx, tx)
	// Сначала счётчик, как в административном хранилище: единый порядок блокировок.
	var meter int64
	err = tx.QueryRow(ctx, `SELECT m.id FROM meters m JOIN premises p ON p.id=m.premises_id WHERE m.id=$2 AND m.deleted_at IS NULL AND `+meterActive+` AND `+residentOwns+` FOR UPDATE OF m`, a.PersonID, in.MeterID).Scan(&meter)
	if err != nil {
		return model.MeterReading{}, mapErr(err)
	}
	var window model.ResidentWindow
	err = tx.QueryRow(ctx, `SELECT w.building_id,w.period,w.opens_on,w.closes_on FROM resident_reading_windows w JOIN premises p ON p.building_id=w.building_id JOIN meters m ON m.premises_id=p.id WHERE m.id=$1 AND (now() AT TIME ZONE 'Europe/Moscow')::date BETWEEN w.opens_on AND w.closes_on FOR SHARE OF w`, meter).Scan(&window.BuildingID, &window.Period, &window.OpensOn, &window.ClosesOn)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.MeterReading{}, invalid("reading window is closed")
		}
		return model.MeterReading{}, err
	}
	if !in.Period.Equal(window.Period.Time) {
		return model.MeterReading{}, invalid("period: only the open period is allowed")
	}
	var today time.Time
	if err = tx.QueryRow(ctx, `SELECT (now() AT TIME ZONE 'Europe/Moscow')::date`).Scan(&today); err != nil {
		return model.MeterReading{}, err
	}
	in.ReadingDate = model.Date{Time: today}
	in.Source = "resident"
	var id int64
	var author *int64
	var source string
	var before []byte
	var version int64
	err = tx.QueryRow(ctx, `SELECT id,resident_account_id,source,to_jsonb(r),COALESCE((SELECT max(id) FROM resident_reading_audit WHERE reading_id=r.id),0) FROM meter_readings r WHERE meter_id=$1 AND period=$2 AND deleted_at IS NULL FOR UPDATE`, meter, in.Period).Scan(&id, &author, &source, &before, &version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return model.MeterReading{}, err
	}
	if id != 0 && (source != "resident" || author == nil || *author != a.ID) {
		return model.MeterReading{}, &Error{ErrConflict, "reading already submitted; contact management"}
	}
	if id != 0 && (expected == nil || *expected != version) {
		return model.MeterReading{}, &Error{ErrConflict, "reading changed; refresh and try again"}
	}
	if id == 0 && expected != nil {
		return model.MeterReading{}, &Error{ErrConflict, "reading changed; refresh and try again"}
	}
	if err = checkReading(ctx, tx, in, id); err != nil {
		return model.MeterReading{}, err
	}
	if id == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO meter_readings(meter_id,reading_date,period,t1,t2,t3,source,comment,resident_account_id) VALUES($1,$2,$3,$4,$5,$6,'resident',$7,$8) RETURNING id`, meter, in.ReadingDate, in.Period, in.T1, in.T2, in.T3, in.Comment, a.ID).Scan(&id)
	} else {
		_, err = tx.Exec(ctx, `UPDATE meter_readings SET reading_date=$2,t1=$3,t2=$4,t3=$5,comment=$6,updated_at=now() WHERE id=$1`, id, in.ReadingDate, in.T1, in.T2, in.T3, in.Comment)
	}
	if err != nil {
		return model.MeterReading{}, mapErr(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO resident_reading_audit(reading_id,account_id,before_values,after_values) SELECT id,$2,$3::jsonb,to_jsonb(r) FROM meter_readings r WHERE id=$1`, id, a.ID, json.RawMessage(before))
	if err != nil {
		return model.MeterReading{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.MeterReading{}, mapErr(err)
	}
	return NewMeterReadings(s.pool).Get(ctx, id)
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
func (s *Residents) SetWindow(ctx context.Context, w model.ResidentWindow) error {
	tag, err := s.pool.Exec(ctx, `INSERT INTO resident_reading_windows(building_id,period,opens_on,closes_on) SELECT id,$2,$3,$4 FROM buildings WHERE id=$1 AND deleted_at IS NULL ON CONFLICT(building_id) DO UPDATE SET period=excluded.period,opens_on=excluded.opens_on,closes_on=excluded.closes_on,updated_at=now()`, w.BuildingID, w.Period, w.OpensOn, w.ClosesOn)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return mapErr(err)
}

// FlushMail отправляет не более одного письма; блокировка защищает от второго worker.
func (s *Residents) FlushMail(ctx context.Context, send func(context.Context, string, string) error) error {
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
	var email, link string
	err = tx.QueryRow(ctx, `SELECT id,email,link FROM resident_mail_outbox WHERE next_attempt_at<=now() ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &email, &link)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if err = send(ctx, email, link); err != nil {
		_, e := tx.Exec(ctx, `UPDATE resident_mail_outbox SET attempts=attempts+1,next_attempt_at=now()+interval '1 minute' WHERE id=$1`, id)
		if e != nil {
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		return errors.New("resident mail delivery failed")
	}
	_, err = tx.Exec(ctx, `DELETE FROM resident_mail_outbox WHERE id=$1`, id)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Residents) Access(ctx context.Context, person int64) (map[string]any, error) {
	var email *string
	var enabled, registered bool
	err := s.pool.QueryRow(ctx, `SELECT ra.email,COALESCE(ra.enabled,false),EXISTS(SELECT 1 FROM resident_accounts a WHERE a.person_id=p.id) FROM persons p LEFT JOIN resident_access ra ON ra.person_id=p.id WHERE p.id=$1 AND p.deleted_at IS NULL`, person).Scan(&email, &enabled, &registered)
	if err != nil {
		return nil, mapErr(err)
	}
	return map[string]any{"email": email, "enabled": enabled, "registered": registered}, nil
}
func (s *Residents) Window(ctx context.Context, building int64) (*model.ResidentWindow, error) {
	w, err := one[model.ResidentWindow](s.pool.Query(ctx, `SELECT building_id,period,opens_on,closes_on FROM resident_reading_windows WHERE building_id=$1`, building))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}
