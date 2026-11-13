package store

import (
	"context"
	"dom-backend/internal/model"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestGateBatch(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	s := NewGate(pool)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "gate-batch"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "gate-batch"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	owner, err := NewPersons(pool).Create(ctx, model.Person{FirstName: "Batch", LastName: "Owner", Phones: []string{}, Emails: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "persons", owner.ID)
	property := func(kind, num string, owned bool) int64 {
		p, e := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: kind, Number: num})
		if e != nil {
			t.Fatal(e)
		}
		cleanup(t, pool, "premises", p.ID)
		if owned {
			o, e := NewOwnerships(pool).Create(ctx, model.Ownership{PremisesID: p.ID, PersonID: &owner.ID, ShareNum: 1, ShareDen: 1, ValidFrom: date(2020, 1, 1)})
			if e != nil {
				t.Fatal(e)
			}
			cleanup(t, pool, "ownerships", o.ID)
		}
		return p.ID
	}
	apartment := property("apartment", "16", true)
	parking := property("parking_space", "178", true)
	foreign := property("apartment", "99", false)
	guard, err := NewUsers(pool).Create(ctx, "gate-batch-guard", "unused", model.RoleGuard)
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "users", guard.ID)
	in := model.GateBatchInput{PremisesIDs: []int64{parking, apartment}, Phone: "+7 926 793 63 63", RecipientName: "Guest", Relation: "guest", NoPersonalVehicle: true}
	rows, err := s.CreateBatch(ctx, owner.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	groupID := rows[0].GroupID
	t.Cleanup(func() {
		if _, e := pool.Exec(ctx, `DELETE FROM gate_requests WHERE group_id=$1`, groupID); e != nil {
			t.Error(e)
		}
	})
	if len(rows) != 2 || rows[1].GroupID != groupID || rows[0].Zone != "courtyard" || rows[1].Zone != "parking" || !rows[0].NoPersonalVehicle {
		t.Fatalf("batch rows: %+v", rows)
	}
	listed, err := s.List(ctx, owner.ID, model.GateFilter{Zone: "parking"}, 1, 0)
	if err != nil || len(listed) != 2 {
		t.Fatalf("filtered group must include both objects: %+v %v", listed, err)
	}
	next, err := s.List(ctx, owner.ID, model.GateFilter{}, 1, 1)
	if err != nil || len(next) != 0 {
		t.Fatalf("pagination must count groups: %+v %v", next, err)
	}
	// Решение по любому объекту заявки применяется к группе.
	if _, err = s.Act(ctx, 0, guard, rows[1].ID, "approve", "Проверено"); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		v, e := s.get(ctx, owner.ID, r.ID)
		if e != nil || !v.Active {
			t.Fatalf("batch approval: %+v %v", v, e)
		}
	}
	events, err := s.Events(ctx, owner.ID, rows[1].ID)
	if err != nil || len(events) != 2 {
		t.Fatalf("group history must not duplicate events: %+v %v", events, err)
	}
	if _, err = s.Act(ctx, owner.ID, model.User{}, groupID, "revoke", ""); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		v, e := s.get(ctx, owner.ID, r.ID)
		if e != nil || v.Active || v.Status != "revoked" {
			t.Fatalf("batch revocation: %+v %v", v, e)
		}
	}
	// Создаём конфликт на последнем объекте: первый объект и его событие должны откатиться.
	single, err := s.Create(ctx, owner.ID, model.GateInput{PremisesID: parking, Zone: "parking", Phone: in.Phone, RecipientName: "Guest", Relation: "guest"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "gate_requests", single.ID)
	var before, after int
	pool.QueryRow(ctx, `SELECT count(*) FROM gate_requests WHERE person_id=$1`, owner.ID).Scan(&before)
	if _, err = s.CreateBatch(ctx, owner.ID, in); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate batch: %v", err)
	}
	pool.QueryRow(ctx, `SELECT count(*) FROM gate_requests WHERE person_id=$1`, owner.ID).Scan(&after)
	if before != after {
		t.Fatal("partial batch was committed")
	}
	bad := in
	bad.PremisesIDs = []int64{apartment, foreign}
	if _, err = s.CreateBatch(ctx, owner.ID, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign object accepted: %v", err)
	}
	bad = in
	bad.PremisesIDs = []int64{apartment, apartment}
	if _, err = s.CreateBatch(ctx, owner.ID, bad); err == nil {
		t.Fatal("duplicate property accepted")
	}
	bad = in
	bad.PremisesIDs = nil
	if _, err = s.CreateBatch(ctx, owner.ID, bad); err == nil {
		t.Fatal("empty selection accepted")
	}
	bad = in
	bad.VehiclePlate = "А123АА"
	if _, err = s.CreateBatch(ctx, owner.ID, bad); err == nil {
		t.Fatal("car and no personal car accepted together")
	}
	// Обратный порядок объектов в двух конкурентных отправках не создаёт повторов или взаимной блокировки.
	concurrent := in
	concurrent.Phone = "79991234571"
	type result struct {
		rows []model.GateRequest
		err  error
	}
	out := make(chan result, 2)
	concurrentCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, ids := range [][]int64{{apartment, parking}, {parking, apartment}} {
		wg.Add(1)
		go func(selected []int64) {
			defer wg.Done()
			input := concurrent
			input.PremisesIDs = selected
			items, e := s.CreateBatch(concurrentCtx, owner.ID, input)
			out <- result{items, e}
		}(ids)
	}
	wg.Wait()
	close(out)
	success, conflict := 0, 0
	for v := range out {
		if v.err == nil {
			success++
			gid := v.rows[0].GroupID
			t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM gate_requests WHERE group_id=$1`, gid) })
		} else if errors.Is(v.err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(v.err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent groups: %d/%d", success, conflict)
	}
	// Утрата права на один объект запрещает одобрение всей ожидающей заявки.
	lost := in
	lost.Phone = "79991234572"
	waiting, err := s.CreateBatch(ctx, owner.ID, lost)
	if err != nil {
		t.Fatal(err)
	}
	waitingID := waiting[0].GroupID
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM gate_requests WHERE group_id=$1`, waitingID) })
	if _, err = pool.Exec(ctx, `UPDATE ownerships SET valid_to='2020-02-01' WHERE premises_id=$1`, apartment); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Act(ctx, 0, guard, waitingID, "approve", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("partially ineligible group approved: %v", err)
	}
	for _, r := range waiting {
		v, e := s.get(ctx, owner.ID, r.ID)
		if e != nil || v.Status != "pending" || v.Active {
			t.Fatalf("partial approval committed: %+v %v", v, e)
		}
	}

}
