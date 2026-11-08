package store

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"dom-backend/internal/model"
)

func TestResidentLifecycleAndIsolation(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewResidents(pool)
	// Тест использует отдельные записи, очистка каскадов выполняется перед общей очисткой.
	org, e := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "resident-test"})
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, e := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "resident-test"})
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "buildings", b.ID)
	p, e := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "1"})
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "premises", p.ID)
	// ID человека не должен случайно совпадать с ID помещения в проверке статуса.
	other, e := NewPersons(pool).Create(ctx, model.Person{LastName: "Без недвижимости", FirstName: "Тест", Phones: []string{}, Emails: []string{}})
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "persons", other.ID)
	person, e := NewPersons(pool).Create(ctx, model.Person{LastName: "Тест", FirstName: "Житель", Phones: []string{}, Emails: []string{"resident@example.test"}})
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "persons", person.ID)
	o, e := NewOwnerships(pool).Create(ctx, model.Ownership{PremisesID: p.ID, PersonID: &person.ID, ShareNum: 1, ShareDen: 1, ValidFrom: date(2020, 1, 1)})
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "ownerships", o.ID)
	m, e := NewMeters(pool).Create(ctx, model.Meter{PremisesID: &p.ID, Resource: "cold_water", Tariffs: 1, SerialNumber: "resident-test", Coefficient: 1, InstalledAt: date(2020, 1, 1), InitialT1: 100})
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "meters", m.ID)
	var admin int64
	e = pool.QueryRow(ctx, `INSERT INTO users(username,password_hash,role) VALUES('resident-test-admin','unused','admin') RETURNING id`).Scan(&admin)
	if e != nil {
		t.Fatal(e)
	}
	cleanup(t, pool, "users", admin)
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM resident_reading_audit WHERE reading_id IN(SELECT id FROM meter_readings WHERE meter_id=$1)`, `DELETE FROM meter_readings WHERE meter_id=$1`} {
			if _, e := pool.Exec(ctx, q, m.ID); e != nil {
				t.Error(e)
			}
		}
		for _, q := range []string{`DELETE FROM resident_sessions WHERE account_id IN(SELECT id FROM resident_accounts WHERE person_id=$1)`, `DELETE FROM resident_accounts WHERE person_id=$1`, `DELETE FROM resident_tokens WHERE person_id=$1`, `DELETE FROM resident_access WHERE person_id=$1`} {
			if _, e := pool.Exec(ctx, q, person.ID); e != nil {
				t.Error(e)
			}
		}
		pool.Exec(ctx, `DELETE FROM resident_reading_windows WHERE building_id=$1`, b.ID)
		pool.Exec(ctx, `DELETE FROM resident_mail_outbox WHERE email='resident@example.test'`)
	})
	assertStatus := func(configured, enabled, registered, entered, eligible bool) {
		t.Helper()
		states, err := s.ListAccess(ctx, []int64{person.ID, -999})
		if err != nil || len(states) != 1 {
			t.Fatalf("status batch: %+v %v", states, err)
		}
		v := states[0]
		if v.Configured != configured || v.Enabled != enabled || v.Registered != registered || (v.LastLoginAt != nil) != entered || v.Eligible != eligible {
			t.Fatalf("unexpected status: %+v", v)
		}
		detail, err := s.Access(ctx, person.ID)
		if err != nil || detail["enabled"] != enabled || detail["registered"] != registered {
			t.Fatalf("status detail: %+v %v", detail, err)
		}
	}
	foreign, e := s.ListAccess(ctx, []int64{other.ID})
	if e != nil || len(foreign) != 1 || foreign[0].Eligible {
		t.Fatalf("person without ownership: %+v %v", foreign, e)
	}
	assertStatus(false, false, false, false, true)
	access, e := s.Access(ctx, person.ID)
	if e != nil || access["enabled"] != false {
		t.Fatalf("initial access: %v %v", access, e)
	}
	if e = s.Approve(ctx, -1, admin, "absent@example.test", true); !errors.Is(e, ErrNotFound) {
		t.Fatalf("absent person: %v", e)
	}
	if e = s.RequestToken(ctx, "resident@example.test", "activate", "https://lk.example.test"); e != nil {
		t.Fatal(e)
	}
	var count int
	pool.QueryRow(ctx, `SELECT count(*) FROM resident_tokens WHERE person_id=$1`, person.ID).Scan(&count)
	if count != 0 {
		t.Fatal("unapproved email got a token")
	}
	if e = s.Approve(ctx, person.ID, admin, "resident@example.test", true); e != nil {
		t.Fatal(e)
	}
	assertStatus(true, true, false, false, true)
	if e = s.RequestToken(ctx, "resident@example.test", "activate", "https://lk.example.test"); e != nil {
		t.Fatal(e)
	}
	var link string
	e = pool.QueryRow(ctx, `SELECT link FROM resident_mail_outbox WHERE email='resident@example.test' ORDER BY id DESC LIMIT 1`).Scan(&link)
	if e != nil {
		t.Fatal(e)
	}
	parsed, _ := url.Parse(link)
	params, _ := url.ParseQuery(strings.SplitN(parsed.Fragment, "?", 2)[1])
	raw := params.Get("token")
	redemptionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { results <- s.RedeemToken(redemptionCtx, raw, "hash-one") }()
	}
	successes, failures := 0, 0
	for i := 0; i < 2; i++ {
		e := <-results
		if e == nil {
			successes++
		} else if errors.Is(e, ErrInvalid) {
			failures++
		} else {
			t.Fatal(e)
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatal("concurrent token consumption", successes, failures)
	}
	if e = s.RedeemToken(ctx, raw, "hash-two"); e == nil {
		t.Fatal("token reused")
	}
	assertStatus(true, true, true, false, true)
	a, e := s.ByEmail(ctx, "resident@example.test")
	if e != nil {
		t.Fatal(e)
	}
	session, e := s.NewSession(ctx, a.ID, a.PasswordHash)
	if e != nil {
		t.Fatal(e)
	}
	assertStatus(true, true, true, true, true)
	if _, e = s.Session(ctx, session); e != nil {
		t.Fatal(e)
	}
	props, e := s.Properties(ctx, a.PersonID)
	if e != nil || len(props) != 1 {
		t.Fatalf("properties: %d %v", len(props), e)
	}
	meters, e := s.Meters(ctx, a.PersonID)
	if e != nil || len(meters) != 1 {
		t.Fatalf("meters: %d %v", len(meters), e)
	}
	today := time.Now().UTC()
	d := model.NewDate(today.Year(), today.Month(), today.Day())
	period := d.FirstOfMonth()
	if e = s.SetWindow(ctx, model.ResidentWindow{BuildingID: b.ID, Period: period, OpensOn: model.Date{Time: d.AddDate(0, 0, -1)}, ClosesOn: model.Date{Time: d.AddDate(0, 0, 1)}}); e != nil {
		t.Fatal(e)
	}
	window, e := s.Window(ctx, b.ID)
	if e != nil || window == nil || !window.Period.Equal(period.Time) {
		t.Fatalf("admin window: %v %v", window, e)
	}
	windows, e := s.Windows(ctx, a.PersonID)
	if e != nil || len(windows) != 1 {
		t.Fatalf("windows: %d %v", len(windows), e)
	}
	reading := model.MeterReading{MeterID: m.ID, Period: period, T1: 100, Comment: ptr("На даче")}
	if _, e = s.SubmitReading(ctx, model.ResidentAccount{ID: a.ID, PersonID: -1}, reading, nil); !errors.Is(e, ErrNotFound) {
		t.Fatalf("foreign write: %v", e)
	}
	if _, e = s.Readings(ctx, model.ResidentAccount{ID: a.ID, PersonID: -1}, m.ID, 50, 0); !errors.Is(e, ErrNotFound) {
		t.Fatalf("foreign read: %v", e)
	}
	saved, e := s.SubmitReading(ctx, a, reading, nil)
	if e != nil {
		t.Fatal(e)
	}
	if saved.DeltaT1 == nil || *saved.DeltaT1 != 0 {
		t.Fatal("zero consumption failed")
	}
	readings, e := s.Readings(ctx, a, m.ID, 50, 0)
	if e != nil || len(readings) != 1 || !readings[0].Editable {
		t.Fatalf("readings: %+v %v", readings, e)
	}
	rev := readings[0].Revision
	reading.T1 = 110
	if _, e = s.SubmitReading(ctx, a, reading, &rev); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SubmitReading(ctx, a, reading, &rev); !errors.Is(e, ErrConflict) {
		t.Fatalf("stale update: %v", e)
	}
	readings, e = s.Readings(ctx, a, m.ID, 50, 0)
	if e != nil {
		t.Fatal(e)
	}
	rev = readings[0].Revision
	reading.T1 = 99
	if _, e = s.SubmitReading(ctx, a, reading, &rev); !errors.Is(e, ErrInvalid) {
		t.Fatalf("decreasing reading: %v", e)
	}
	audit, e := s.Audit(ctx, a, m.ID)
	if e != nil || len(audit) != 2 {
		t.Fatalf("audit: %d %v", len(audit), e)
	}

	// Запрет закрытого периода и правки записи, которую принял сотрудник.
	reading.T1 = 110
	wrong := reading
	wrong.Period = model.Date{Time: period.AddDate(0, -1, 0)}
	if _, e = s.SubmitReading(ctx, a, wrong, &rev); !errors.Is(e, ErrInvalid) {
		t.Fatalf("wrong period: %v", e)
	}
	if _, e = pool.Exec(ctx, `UPDATE meter_readings SET source='manual' WHERE id=$1`, saved.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SubmitReading(ctx, a, reading, &rev); !errors.Is(e, ErrConflict) {
		t.Fatalf("staff reading overwritten: %v", e)
	}
	if e = s.SetWindow(ctx, model.ResidentWindow{BuildingID: b.ID, Period: period, OpensOn: model.Date{Time: d.AddDate(0, 0, -2)}, ClosesOn: model.Date{Time: d.AddDate(0, 0, -1)}}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SubmitReading(ctx, a, reading, &rev); !errors.Is(e, ErrInvalid) {
		t.Fatalf("closed window: %v", e)
	}
	if e = s.ChangePassword(ctx, a.ID, "hash-one", "hash-two"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Session(ctx, session); !errors.Is(e, ErrNotFound) {
		t.Fatalf("old session survived: %v", e)
	}

	if _, e = s.NewSession(ctx, a.ID, "hash-one"); !errors.Is(e, ErrNotFound) {
		t.Fatalf("stale password created session: %v", e)
	}
	if e = s.Approve(ctx, person.ID, admin, "other@example.test", true); !errors.Is(e, ErrInvalid) {
		t.Fatalf("login email replaced through contact approval: %v", e)
	}
	// Восстановление пароля и истечение ссылок.
	if e = s.RequestToken(ctx, "resident@example.test", "reset", "https://lk.example.test"); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `SELECT link FROM resident_mail_outbox WHERE email='resident@example.test' ORDER BY id DESC LIMIT 1`).Scan(&link); e != nil {
		t.Fatal(e)
	}
	parsed, _ = url.Parse(link)
	params, _ = url.ParseQuery(strings.SplitN(parsed.Fragment, "?", 2)[1])
	raw = params.Get("token")
	if _, e = pool.Exec(ctx, `UPDATE resident_tokens SET expires_at=now()-interval '1 minute' WHERE token_hash=$1`, ResidentTokenHash(raw)); e != nil {
		t.Fatal(e)
	}
	if e = s.RedeemToken(ctx, raw, "hash-three"); !errors.Is(e, ErrInvalid) {
		t.Fatalf("expired token redeemed: %v", e)
	}
	if e = s.RequestToken(ctx, "resident@example.test", "reset", "https://lk.example.test"); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `SELECT link FROM resident_mail_outbox WHERE email='resident@example.test' ORDER BY id DESC LIMIT 1`).Scan(&link); e != nil {
		t.Fatal(e)
	}
	parsed, _ = url.Parse(link)
	params, _ = url.ParseQuery(strings.SplitN(parsed.Fragment, "?", 2)[1])
	raw = params.Get("token")
	if e = s.RedeemToken(ctx, raw, "hash-three"); e != nil {
		t.Fatal(e)
	}
	a, e = s.ByEmail(ctx, "resident@example.test")
	if e != nil || a.PasswordHash != "hash-three" {
		t.Fatalf("reset: %v", e)
	}
	session, e = s.NewSession(ctx, a.ID, a.PasswordHash)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `UPDATE ownerships SET valid_to=(now() AT TIME ZONE 'Europe/Moscow')::date-1 WHERE id=$1`, o.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Session(ctx, session); !errors.Is(e, ErrNotFound) {
		t.Fatalf("former owner retains access: %v", e)
	}
	assertStatus(true, true, true, true, false)
	if e = s.Approve(ctx, person.ID, admin, "resident@example.test", false); e != nil {
		t.Fatal(e)
	}
	assertStatus(true, false, true, true, false)
	if _, e = s.ByEmail(ctx, "resident@example.test"); !errors.Is(e, ErrNotFound) {
		t.Fatalf("disabled access: %v", e)
	}
}
