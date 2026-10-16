package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

func TestAccountServices(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	services, assigned := NewServices(pool), NewAccountServices(pool)

	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "acc-services-test"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "acc-services-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM account_services WHERE account_id IN (SELECT a.id FROM personal_accounts a JOIN premises p ON p.id = a.premises_id WHERE p.building_id = $1)`, b.ID)
		pool.Exec(ctx, `DELETE FROM personal_accounts WHERE premises_id IN (SELECT id FROM premises WHERE building_id = $1)`, b.ID)
		pool.Exec(ctx, `DELETE FROM premises WHERE building_id = $1`, b.ID)
		pool.Exec(ctx, `DELETE FROM service_tariffs WHERE service_id IN (SELECT id FROM services WHERE organization_id = $1)`, org.ID)
		pool.Exec(ctx, `DELETE FROM services WHERE organization_id = $1`, org.ID)
		pool.Exec(ctx, `DELETE FROM buildings WHERE id = $1`, b.ID)
		pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, org.ID)
	})

	// Две квартиры и машиноместо, у каждого счёт ЖКУ; у квартиры 1 ещё счёт капремонта.
	account := func(kind, number, purpose string) model.Account {
		t.Helper()
		pr, err := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: kind, Number: number})
		if err != nil {
			t.Fatal(err)
		}
		return accountFor(t, ctx, pool, pr.ID, number+"-"+purpose, purpose)
	}
	a1 := account("apartment", "1", "utilities")
	a2 := account("apartment", "2", "utilities")
	p1 := a1.PremisesID
	capital := accountFor(t, ctx, pool, p1, "1-capital", "capital_repair")
	park := account("parking_space", "P1", "utilities")

	svc := func(in model.Service) model.Service {
		t.Helper()
		in.OrganizationID, in.Active = org.ID, true
		in.SetDefaults()
		out, err := services.Create(ctx, in)
		if err != nil {
			t.Fatalf("create %s: %v", in.Name, err)
		}
		return out
	}
	upkeep := svc(model.Service{Kind: "maintenance", Name: "Содержание — квартиры", Unit: "m2", CalcMethod: "per_area", PremisesKinds: []string{"apartment"}, ReceiptName: ptr("Содержание и ремонт")})
	if upkeep.ReceiptName == nil || *upkeep.ReceiptName != "Содержание и ремонт" || upkeep.AccountPurpose != "utilities" || len(upkeep.PremisesKinds) != 1 {
		t.Errorf("new service fields: %+v", upkeep)
	}
	fund := svc(model.Service{Kind: "maintenance", Name: "Взнос на капремонт", Unit: "m2", CalcMethod: "per_area", AccountPurpose: "capital_repair"})
	keys := svc(model.Service{Kind: "additional", Name: "Изготовление ключей", Unit: "piece", CalcMethod: "manual"})

	from := date(2026, 10, 1)
	assign := func(acc, service int64, from model.Date, to *model.Date, q *float64) (model.AccountService, error) {
		return assigned.Create(ctx, model.AccountService{AccountID: acc, ServiceID: service, ValidFrom: from, ValidTo: to, Quantity: q})
	}
	x, err := assign(a1.ID, upkeep.ID, from, nil, nil)
	if err != nil || x.AccountNumber != "as-test-1-utilities" || x.ServiceName != "Содержание — квартиры" || x.PremisesNumber != "1" || x.BuildingAddress != "acc-services-test" {
		t.Fatalf("assign: %+v, %v", x, err)
	}
	// Ограничения БД.
	for name, c := range map[string]struct {
		acc, svc int64
		q        *float64
		want     string
	}{
		"purpose mismatch":       {capital.ID, upkeep.ID, nil, "purpose does not match"},
		"wrong kind":             {park.ID, upkeep.ID, nil, "not available for this premises kind"},
		"manual no qty":          {a2.ID, keys.ID, nil, "quantity is required"},
		"qty with wrong purpose": {a2.ID, fund.ID, ptr(2.0), "does not match"}, // тип счёта проверяется раньше количества
		"qty on per_area":        {a2.ID, upkeep.ID, ptr(2.0), "quantity must be empty"},
	} {
		if _, err := assign(c.acc, c.svc, from, nil, c.q); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
	if _, err := assign(a1.ID, upkeep.ID, date(2026, 11, 1), nil, nil); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "account_services_no_overlap") {
		t.Errorf("overlap: %v", err)
	}
	// Разовая услуга: октябрь и ноябрь отдельно, один месяц каждая.
	for _, m := range []time.Month{10, 11} {
		if _, err := assign(a1.ID, keys.ID, date(2026, m, 1), ptr(date(2026, m, 28)), ptr(2.0)); err != nil {
			t.Fatalf("one-off %d: %v", m, err)
		}
	}
	list, err := assigned.List(ctx, model.AccountServiceFilter{AccountID: &a1.ID, ActiveOn: ptr(date(2026, 11, 15))}, 10, 0)
	if err != nil || len(list) != 2 {
		t.Errorf("active on nov 15: %d, %v", len(list), err)
	}

	// Массовое назначение: счета ЖКУ квартир, у которых услуги ещё нет (счёт 1 уже имеет).
	req := model.AccountServiceBulkRequest{ServiceID: upkeep.ID, ValidFrom: from, BuildingID: &b.ID}
	pv, err := assigned.BulkPreview(ctx, req)
	if err != nil || pv.Candidates != 2 || pv.ToAssign != 1 || pv.SkippedExisting != 1 || len(pv.Sample) != 1 || pv.Sample[0].PremisesNumber != "2" {
		t.Fatalf("preview: %+v, %v", pv, err)
	}
	res, err := assigned.BulkAssign(ctx, req)
	if err != nil || res.Created != 1 {
		t.Fatalf("bulk: %+v, %v", res, err)
	}
	if res, err := assigned.BulkAssign(ctx, req); err != nil || res.Created != 0 {
		t.Errorf("repeat bulk: %+v, %v", res, err)
	}
	// Капремонт идёт только на счета капремонта; ручная услуга требует количество.
	if pv, err := assigned.BulkPreview(ctx, model.AccountServiceBulkRequest{ServiceID: fund.ID, ValidFrom: from, BuildingID: &b.ID}); err != nil || pv.ToAssign != 1 {
		t.Errorf("capital preview: %+v, %v", pv, err)
	}
	if _, err := assigned.BulkPreview(ctx, model.AccountServiceBulkRequest{ServiceID: keys.ID, ValidFrom: from}); !errors.Is(err, ErrInvalid) {
		t.Errorf("manual without quantity: %v", err)
	}

	// Нельзя менять услугу так, чтобы назначения стали несовместимыми, и удалять услугу/счёт с назначениями.
	bad := upkeep
	bad.AccountPurpose = "capital_repair"
	if _, err := services.Update(ctx, upkeep.ID, bad); !errors.Is(err, ErrInvalid) {
		t.Errorf("change purpose with assignments: %v", err)
	}
	if err := services.Delete(ctx, upkeep.ID); err == nil {
		t.Error("delete service with assignments: want error")
	}
	if err := NewAccounts(pool).Delete(ctx, a1.ID); err == nil {
		t.Error("delete account with assignments: want error")
	}

	// Прекращение услуги: закрыть с даты; выключенной услуге новые назначения нельзя, а закрыть старые можно.
	off := upkeep
	off.Active = false
	if _, err := services.Update(ctx, upkeep.ID, off); err != nil {
		t.Fatal(err)
	}
	if _, err := assign(a2.ID, upkeep.ID, date(2027, 1, 1), nil, nil); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "not active") {
		t.Errorf("inactive service: %v", err)
	}
	cl, err := assigned.BulkClose(ctx, model.AccountServiceCloseRequest{ServiceID: upkeep.ID, ValidTo: date(2026, 12, 31), BuildingID: &b.ID})
	if err != nil || cl.Closed != 2 {
		t.Fatalf("close: %+v, %v", cl, err)
	}
	if cl, err := assigned.BulkClose(ctx, model.AccountServiceCloseRequest{ServiceID: upkeep.ID, ValidTo: date(2026, 12, 31)}); err != nil || cl.Closed != 0 {
		t.Errorf("repeat close: %+v, %v", cl, err)
	}
	got, err := assigned.Get(ctx, x.ID)
	if err != nil || got.ValidTo == nil || got.ValidTo.Format("2006-01-02") != "2026-12-31" {
		t.Errorf("closed assignment: %+v, %v", got, err)
	}
}

func accountFor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, premisesID int64, number, purpose string) model.Account {
	t.Helper()
	a, err := NewAccounts(pool).Create(ctx, model.Account{Number: "as-test-" + number, PremisesID: premisesID, Purpose: purpose, Status: "active", OpenedAt: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
