package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dom-backend/internal/calc"
	"dom-backend/internal/model"
)

func TestAccountCalculation(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	svcs, tariffs, norms := NewServices(pool), NewServiceTariffs(pool), NewServiceNorms(pool)
	assigned, meters, readings, calculator := NewAccountServices(pool), NewMeters(pool), NewMeterReadings(pool), NewCalculator(pool)

	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "calc-test"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "calc-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM service_meters WHERE service_id IN (SELECT id FROM services WHERE organization_id = $1)`, org.ID)
		pool.Exec(ctx, `DELETE FROM account_services WHERE account_id IN (SELECT a.id FROM personal_accounts a JOIN premises p ON p.id = a.premises_id WHERE p.building_id = $1)`, b.ID)
		pool.Exec(ctx, `DELETE FROM meter_readings WHERE meter_id IN (SELECT id FROM meters WHERE building_id = $1 OR premises_id IN (SELECT id FROM premises WHERE building_id = $1))`, b.ID)
		pool.Exec(ctx, `DELETE FROM meters WHERE building_id = $1 OR premises_id IN (SELECT id FROM premises WHERE building_id = $1)`, b.ID)
		pool.Exec(ctx, `DELETE FROM personal_accounts WHERE premises_id IN (SELECT id FROM premises WHERE building_id = $1)`, b.ID)
		pool.Exec(ctx, `DELETE FROM premises WHERE building_id = $1`, b.ID)
		pool.Exec(ctx, `DELETE FROM service_norms WHERE service_id IN (SELECT id FROM services WHERE organization_id = $1)`, org.ID)
		pool.Exec(ctx, `DELETE FROM service_tariffs WHERE service_id IN (SELECT id FROM services WHERE organization_id = $1)`, org.ID)
		pool.Exec(ctx, `DELETE FROM services WHERE organization_id = $1`, org.ID)
		pool.Exec(ctx, `DELETE FROM buildings WHERE id = $1`, b.ID)
		pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, org.ID)
	})

	// Две квартиры: 50 и 150 м².
	flat := func(number string, area float64) (model.Premises, model.Account) {
		t.Helper()
		pr, err := NewPremisesStore(pool).Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: number, TotalArea: &area})
		if err != nil {
			t.Fatal(err)
		}
		return pr, accountFor(t, ctx, pool, pr.ID, "calc-"+number, "utilities")
	}
	p1, a1 := flat("1", 50)
	_, a2 := flat("2", 150)

	svc := func(in model.Service, price float64) model.Service {
		t.Helper()
		in.OrganizationID, in.Active = org.ID, true
		in.SetDefaults()
		out, err := svcs.Create(ctx, in)
		if err != nil {
			t.Fatalf("create %s: %v", in.Name, err)
		}
		if price > 0 {
			if _, err := tariffs.Create(ctx, model.ServiceTariff{ServiceID: out.ID, Price: price, ValidFrom: date(2026, 1, 1)}); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	one := 1
	upkeep := svc(model.Service{Kind: "maintenance", Name: "Содержание", Unit: "m2", CalcMethod: "per_area", Position: 1}, 30)
	heat := svc(model.Service{Kind: "utility", Name: "Отопление", Unit: "gcal", CalcMethod: "per_area_norm", Position: 2}, 2000)
	cold := svc(model.Service{Kind: "utility", Name: "ХВС", Unit: "m3", CalcMethod: "individual_meter", MeterResource: ptr("cold_water"), MeterTariff: &one, Position: 3}, 40)
	odn := svc(model.Service{Kind: "maintenance", Name: "Электроэнергия ОДН", Unit: "kwh", CalcMethod: "common_meter", MeterResource: ptr("electricity"), MeterTariff: &one, Position: 4}, 6)
	if _, err := norms.Create(ctx, model.ServiceNorm{ServiceID: heat.ID, BuildingID: b.ID, Value: 0.011, ValidFrom: date(2026, 1, 1)}); err != nil {
		t.Fatal(err)
	}

	// Счётчики: индивидуальный ХВС в квартире 1 и общедомовой электрический.
	own, err := meters.Create(ctx, model.Meter{PremisesID: &p1.ID, Resource: "cold_water", Tariffs: 1, Coefficient: 1, SerialNumber: "calc-cold", InstalledAt: date(2015, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	common, err := meters.Create(ctx, model.Meter{BuildingID: &b.ID, Resource: "electricity", Tariffs: 1, Coefficient: 1, SerialNumber: "calc-common", InstalledAt: date(2015, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	read := func(m model.Meter, day model.Date, period model.Date, v float64) (model.MeterReading, error) {
		return readings.Create(ctx, model.MeterReading{MeterID: m.ID, ReadingDate: day, Period: period, T1: v})
	}

	// Период показания: по умолчанию месяц даты, но его можно задать явно; в месяце одно показание.
	r1, err := read(own, date(2026, 9, 30), model.Date{}, 100)
	if err != nil || r1.Period != date(2026, 9, 1) {
		t.Fatalf("default period: %+v %v", r1.Period, err)
	}
	if _, err := read(own, date(2026, 9, 5), model.Date{}, 90); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("second reading in a month: %v", err)
	}
	// Показание, снятое 25 октября, привязано к октябрю; период — любая дата месяца, приводится к первому числу.
	r2, err := read(own, date(2026, 10, 25), date(2026, 10, 3), 120.5)
	if err != nil || r2.Period != date(2026, 10, 1) {
		t.Fatalf("explicit period: %+v %v", r2.Period, err)
	}
	if list, err := readings.List(ctx, model.MeterReadingFilter{MeterID: &own.ID, Period: ptr(date(2026, 10, 1))}, 10, 0); err != nil || len(list) != 1 || list[0].ID != r2.ID {
		t.Errorf("filter by period: %+v %v", list, err)
	}
	if _, err := read(common, date(2026, 9, 30), model.Date{}, 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := read(common, date(2026, 10, 25), model.Date{}, 2000); err != nil {
		t.Fatal(err)
	}

	// Общедомовые счётчики услуги: только общедомовые, того же ресурса, в доме организации услуги.
	odn.MeterIDs = []int64{own.ID}
	if _, err := svcs.Update(ctx, odn.ID, odn); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "meter_ids") {
		t.Errorf("individual meter in service: %v", err)
	}
	odn.MeterIDs = []int64{common.ID}
	if odn, err = svcs.Update(ctx, odn.ID, odn); err != nil || len(odn.MeterIDs) != 1 {
		t.Fatalf("link common meter: %+v %v", odn.MeterIDs, err)
	}
	cold.MeterIDs = []int64{common.ID}
	if cold, err = svcs.Update(ctx, cold.ID, cold); err != nil || len(cold.MeterIDs) != 0 {
		t.Errorf("meters are dropped for other methods: %+v %v", cold.MeterIDs, err)
	}

	from := date(2026, 10, 1)
	for _, s := range []model.Service{upkeep, heat, cold, odn} {
		if _, err := assigned.Create(ctx, model.AccountService{AccountID: a1.ID, ServiceID: s.ID, ValidFrom: from}); err != nil {
			t.Fatalf("assign %s: %v", s.Name, err)
		}
	}
	if _, err := assigned.Create(ctx, model.AccountService{AccountID: a2.ID, ServiceID: odn.ID, ValidFrom: date(2026, 10, 15), ValidTo: ptr(date(2026, 10, 20))}); err != nil {
		t.Fatal(err)
	}

	// Октябрь, квартира 1: содержание 50×30 = 1500; отопление 50×0,011×2000 = 1100; ХВС 20,5×40 = 820;
	// ОДН: 1000 кВт·ч × 50/200 = 250 × 6 = 1500 (квартира 2 назначена на часть месяца — это тоже участие). Итого 4920.
	res, err := calculator.AccountCalculation(ctx, a1.ID, date(2026, 10, 20))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name   string
		amount float64
	}{{"Содержание", 1500}, {"Отопление", 1100}, {"ХВС", 820}, {"Электроэнергия ОДН", 1500}}
	if len(res.Lines) != len(want) || res.Total != 4920 || !res.Complete || res.Period != "2026-10-01" || res.AccountNumber != a1.Number {
		t.Fatalf("calculation: %+v", res)
	}
	for i, w := range want {
		l := res.Lines[i]
		if l.Name != w.name || l.Status != calc.StatusOK || l.Amount == nil || *l.Amount != w.amount {
			t.Errorf("line %d: %+v", i, l)
		}
	}
	if m := res.Lines[2].Meters; len(m) != 1 || *m[0].Previous != 100 || *m[0].Current != 120.5 {
		t.Errorf("meter details: %+v", m)
	}

	// Сентябрь: услуги ещё не назначены — пусто. Ноябрь: показаний нет — строки не считаются, итог неполный.
	if res, err = calculator.AccountCalculation(ctx, a1.ID, date(2026, 9, 1)); err != nil || len(res.Lines) != 0 {
		t.Errorf("september: %+v %v", res, err)
	}
	res, err = calculator.AccountCalculation(ctx, a1.ID, date(2026, 11, 1))
	if err != nil || res.Complete {
		t.Fatalf("november: %+v %v", res, err)
	}
	status := map[string]string{}
	for _, l := range res.Lines {
		status[l.Name] = l.Status
	}
	if status["ХВС"] != calc.StatusNoReading || status["Электроэнергия ОДН"] != calc.StatusNoReading || status["Содержание"] != calc.StatusOK || status["Отопление"] != calc.StatusOK {
		t.Errorf("november statuses: %v", status)
	}

	// Несуществующий счёт.
	if _, err := calculator.AccountCalculation(ctx, 0, date(2026, 10, 1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown account: %v", err)
	}
}
