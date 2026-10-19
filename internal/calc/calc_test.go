package calc

import (
	"math/big"
	"testing"
	"time"
)

func r(s string) *big.Rat {
	v, ok := new(big.Rat).SetString(s)
	if !ok {
		panic(s)
	}
	return v
}

func d(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }

var oct = d(2026, 10, 1)

func rd(p time.Time, t ...string) *Reading {
	x := &Reading{Period: p}
	for i, v := range t {
		x.T[i] = r(v)
	}
	return x
}

func meter(id int64, res string, tariffs int, prev, cur *Reading) Meter {
	return Meter{ID: id, Serial: "m" + string(rune('0'+id)), Resource: res, Tariffs: tariffs, Coefficient: r("1"), InstalledAt: d(2015, 1, 1), Previous: prev, Current: cur}
}

func input(area string, services ...*Service) Input {
	in := Input{Period: oct, AccountID: 1, AccountNumber: "100", Services: map[int64]*Service{}}
	if area != "" {
		in.Area = r(area)
	}
	for _, s := range services {
		in.Services[s.ID] = s
		in.Assignments = append(in.Assignments, Assignment{ServiceID: s.ID})
	}
	return in
}

func f(v *float64) float64 {
	if v == nil {
		return -1
	}
	return *v
}

func only(t *testing.T, res Result) Line {
	t.Helper()
	if len(res.Lines) != 1 {
		t.Fatalf("lines: %+v", res.Lines)
	}
	return res.Lines[0]
}

func TestHeatingAreaNorm(t *testing.T) {
	// Примеры из постановки: площадь × норматив × тариф за Гкал.
	for _, c := range []struct {
		area, norm, price string
		qty, amount       float64
	}{
		{"56.5", "0.011", "1970.40", 0.6215, 1224.60},
		{"180.1", "0.011", "2325.88", 1.9811, 4607.80},
	} {
		s := &Service{ID: 1, Name: "Отопление", Unit: "gcal", Method: "per_area_norm", NormFirst: r(c.norm), NormLast: r(c.norm), PriceFirst: r(c.price), PriceLast: r(c.price)}
		res := Compute(input(c.area, s))
		l := only(t, res)
		if l.Status != StatusOK || f(l.Quantity) != c.qty || f(l.Amount) != c.amount || res.Total != c.amount || !res.Complete || len(l.Warnings) != 0 {
			t.Errorf("%s: %+v total %v", c.area, l, res.Total)
		}
	}
	// Нет норматива, нет цены, нет площади.
	s := &Service{ID: 1, Name: "Отопление", Method: "per_area_norm", PriceFirst: r("100")}
	if l := only(t, Compute(input("50", s))); l.Status != StatusNoNorm || l.Amount != nil {
		t.Errorf("no norm: %+v", l)
	}
	s = &Service{ID: 1, Name: "Отопление", Method: "per_area_norm", NormFirst: r("0.011")}
	if l := only(t, Compute(input("50", s))); l.Status != StatusNoPrice {
		t.Errorf("no price: %+v", l)
	}
	if l := only(t, Compute(input("", s))); l.Status != StatusNoArea {
		t.Errorf("no area: %+v", l)
	}
	// Норматив и цена меняются внутри месяца — предупреждения, берём первое число.
	s = &Service{ID: 1, Name: "Отопление", Method: "per_area_norm", NormFirst: r("0.011"), NormLast: r("0.016"), PriceFirst: r("10"), PriceLast: r("12")}
	if l := only(t, Compute(input("10", s))); len(l.Warnings) != 2 || f(l.Price) != 10 {
		t.Errorf("changes: %+v", l)
	}
}

func TestSimpleMethods(t *testing.T) {
	fixed := &Service{ID: 1, Name: "Домофон", Method: "fixed", Unit: "month", PriceFirst: r("100.50")}
	area := &Service{ID: 2, Name: "Содержание", Method: "per_area", Unit: "m2", PriceFirst: r("33.333")}
	manual := &Service{ID: 3, Name: "Ключи", Method: "manual", Unit: "piece", PriceFirst: r("250")}
	in := input("56.5", fixed, area, manual)
	in.Assignments = append(in.Assignments, Assignment{ServiceID: 3, Quantity: r("1")}, Assignment{ServiceID: 3, Quantity: r("2")})
	res := Compute(in)
	// 56.5 × 33.333 = 1883.3145 -> 1883.31; ключи 3 × 250 = 750 (назначения месяца складываются); домофон 100.50
	want := map[int64]float64{1: 100.5, 2: 1883.31, 3: 750}
	if len(res.Lines) != 3 {
		t.Fatalf("lines: %+v", res.Lines)
	}
	sum := 0.0
	for _, l := range res.Lines {
		if l.Status != StatusOK || f(l.Amount) != want[l.ServiceID] {
			t.Errorf("line %d: %+v", l.ServiceID, l)
		}
		sum += f(l.Amount)
	}
	if res.Total != 2733.81 || !res.Complete {
		t.Errorf("total: %v %v", res.Total, sum)
	}
	// Ручная услуга без количества.
	in2 := input("1", manual)
	if l := only(t, Compute(in2)); l.Status != StatusNoQuantity {
		t.Errorf("manual: %+v", l)
	}
}

func TestLinesOrderAndReceiptName(t *testing.T) {
	a := &Service{ID: 1, Name: "Б", Method: "fixed", Position: 2, PriceFirst: r("1")}
	b := &Service{ID: 2, Name: "А", ReceiptName: "Содержание и ремонт", Method: "fixed", Position: 1, PriceFirst: r("1")}
	c := &Service{ID: 3, Name: "В", Method: "fixed", Position: 1, PriceFirst: r("1")}
	res := Compute(input("", a, b, c))
	if res.Lines[0].Name != "Содержание и ремонт" || res.Lines[1].Name != "В" || res.Lines[2].Name != "Б" {
		t.Errorf("order: %+v", res.Lines)
	}
}

func TestIndividualMeter(t *testing.T) {
	sep := d(2026, 9, 1)
	svc := func() *Service {
		return &Service{ID: 1, Name: "ХВС", Method: "individual_meter", Unit: "m3", MeterResource: "cold_water", MeterTariff: 1, PriceFirst: r("45.1234")}
	}
	// Показание октября 120.5, сентября 100 -> 20.5 м³ × 45.1234 = 925.03.
	in := input("50", svc())
	in.Meters = []Meter{meter(1, "cold_water", 1, rd(sep, "100"), rd(oct, "120.5"))}
	l := only(t, Compute(in))
	if l.Status != StatusOK || f(l.Quantity) != 20.5 || f(l.Amount) != 925.03 || len(l.Meters) != 1 || f(l.Meters[0].Consumption) != 20.5 {
		t.Errorf("ok: %+v", l)
	}
	// Два счётчика одного ресурса складываются; коэффициент применяется.
	in = input("50", svc())
	m2 := meter(2, "cold_water", 1, rd(sep, "10"), rd(oct, "12"))
	m2.Coefficient = r("10")
	in.Meters = []Meter{meter(1, "cold_water", 1, rd(sep, "100"), rd(oct, "101")), m2}
	if l := only(t, Compute(in)); f(l.Quantity) != 21 {
		t.Errorf("two meters: %+v", l)
	}
	// Счётчик другого ресурса не учитывается; счётчиков нет вовсе.
	in = input("50", svc())
	in.Meters = []Meter{meter(1, "hot_water", 1, rd(sep, "1"), rd(oct, "2"))}
	if l := only(t, Compute(in)); l.Status != StatusNoMeters {
		t.Errorf("no meters: %+v", l)
	}
	// Показания за месяц не переданы — сумма не считается, итог неполный.
	in = input("50", svc())
	in.Meters = []Meter{meter(1, "cold_water", 1, rd(sep, "100"), nil)}
	res := Compute(in)
	if l := only(t, res); l.Status != StatusNoReading || l.Amount != nil || res.Total != 0 || res.Complete {
		t.Errorf("no reading: %+v", l)
	}
	// Одно из двух не передано — нет частичной суммы.
	in = input("50", svc())
	in.Meters = []Meter{meter(1, "cold_water", 1, rd(sep, "1"), rd(oct, "2")), meter(2, "cold_water", 1, rd(sep, "1"), nil)}
	if l := only(t, Compute(in)); l.Status != StatusNoReading {
		t.Errorf("partial: %+v", l)
	}
	// Первое показание — только точка отсчёта (установлен давно, предыдущего нет).
	in = input("50", svc())
	in.Meters = []Meter{meter(1, "cold_water", 1, nil, rd(oct, "100"))}
	if l := only(t, Compute(in)); l.Status != StatusNoPrevious {
		t.Errorf("first reading: %+v", l)
	}
	// Счётчик установлен в этом месяце — считаем от начальных показаний.
	in = input("50", svc())
	nm := meter(1, "cold_water", 1, nil, rd(oct, "7"))
	nm.InstalledAt = d(2026, 10, 5)
	nm.Initial[0] = r("2")
	if l := only(t, Compute(Input{Period: oct, Area: in.Area, Meters: []Meter{nm}, Assignments: in.Assignments, Services: in.Services})); f(l.Quantity) != 5 {
		t.Errorf("new meter: %+v", l)
	}
	// Показание меньше предыдущего.
	in = input("50", svc())
	in.Meters = []Meter{meter(1, "cold_water", 1, rd(sep, "100"), rd(oct, "90"))}
	if l := only(t, Compute(in)); l.Status != StatusNegative {
		t.Errorf("negative: %+v", l)
	}
	// Снятый до месяца и установленный после месяца счётчики не участвуют.
	in = input("50", svc())
	old := meter(1, "cold_water", 1, rd(sep, "1"), nil)
	gone := d(2026, 9, 20)
	old.RemovedAt = &gone
	later := meter(2, "cold_water", 1, nil, nil)
	later.InstalledAt = d(2026, 11, 1)
	in.Meters = []Meter{old, later, meter(3, "cold_water", 1, rd(sep, "10"), rd(oct, "13"))}
	if l := only(t, Compute(in)); l.Status != StatusOK || f(l.Quantity) != 3 {
		t.Errorf("inactive meters: %+v", l)
	}
}

func TestElectricityTariffs(t *testing.T) {
	sep := d(2026, 9, 1)
	svc := &Service{ID: 1, Name: "Т2", Method: "individual_meter", Unit: "kwh", MeterResource: "electricity", MeterTariff: 2, PriceFirst: r("6.1")}
	in := input("50", svc)
	in.Meters = []Meter{meter(1, "electricity", 3, rd(sep, "1", "100", "10"), rd(oct, "5", "150.5", "12"))}
	if l := only(t, Compute(in)); f(l.Quantity) != 50.5 || f(l.Amount) != 308.05 {
		t.Errorf("t2: %+v", l)
	}
	// У однотарифного счётчика нет Т2.
	in.Meters = []Meter{meter(1, "electricity", 1, rd(sep, "1"), rd(oct, "5"))}
	if l := only(t, Compute(in)); l.Status != StatusNoMeters {
		t.Errorf("single tariff meter: %+v", l)
	}
}

func TestCommonMeter(t *testing.T) {
	sep := d(2026, 9, 1)
	svc := &Service{ID: 1, Name: "ОДН", Method: "common_meter", Unit: "kwh", MeterResource: "electricity", MeterTariff: 1, PriceFirst: r("6"),
		CommonMeters: []Meter{meter(9, "electricity", 1, rd(sep, "1000"), rd(oct, "2000"))}, ShareBase: r("2000")}
	// 1000 кВт·ч на дом, помещение 50 из 2000 м² -> 25 кВт·ч × 6 = 150.
	l := only(t, Compute(input("50", svc)))
	if l.Status != StatusOK || f(l.Quantity) != 25 || f(l.Amount) != 150 || f(l.CommonTotal) != 1000 || f(l.ShareBase) != 2000 {
		t.Errorf("common: %+v", l)
	}
	// Несколько общедомовых счётчиков складываются.
	svc.CommonMeters = append(svc.CommonMeters, meter(10, "electricity", 1, rd(sep, "0"), rd(oct, "1000")))
	if l := only(t, Compute(input("50", svc))); f(l.Quantity) != 50 {
		t.Errorf("two common meters: %+v", l)
	}
	// Нет показания общедомового — не считаем.
	svc.CommonMeters = []Meter{meter(9, "electricity", 1, rd(sep, "1"), nil)}
	if l := only(t, Compute(input("50", svc))); l.Status != StatusNoReading {
		t.Errorf("no reading: %+v", l)
	}
	// Счётчики не выбраны.
	svc.CommonMeters = nil
	if l := only(t, Compute(input("50", svc))); l.Status != StatusNoMeters {
		t.Errorf("no common meters: %+v", l)
	}
	// Нет площади у помещения / нечего делить; предупреждение, если у части помещений нет площади.
	svc.CommonMeters = []Meter{meter(9, "electricity", 1, rd(sep, "1000"), rd(oct, "2000"))}
	if l := only(t, Compute(input("", svc))); l.Status != StatusNoArea {
		t.Errorf("no area: %+v", l)
	}
	svc.ShareNoArea = 3
	if l := only(t, Compute(input("50", svc))); l.Status != StatusOK || len(l.Warnings) != 1 || l.Warnings[0] != WarnAreaMissing {
		t.Errorf("area missing warning: %+v", l)
	}
	svc.ShareBase = nil
	if l := only(t, Compute(input("50", svc))); l.Status != StatusNoArea {
		t.Errorf("no base: %+v", l)
	}
}

func TestSumVolumesAndSumPrices(t *testing.T) {
	sep := d(2026, 9, 1)
	cold := &Service{ID: 1, Name: "ХВС", Method: "individual_meter", Unit: "m3", MeterResource: "cold_water", MeterTariff: 1, PriceFirst: r("40")}
	hot := &Service{ID: 2, Name: "ХВС для ГВС", Method: "individual_meter", Unit: "m3", MeterResource: "hot_water", MeterTariff: 1, PriceFirst: r("10")}
	disposal := &Service{ID: 3, Name: "Водоотведение", Method: "sum_volumes", Unit: "m3", ComponentIDs: []int64{1, 2}, PriceFirst: r("30.5")}
	both := &Service{ID: 4, Name: "Вода", Method: "sum_prices", Unit: "m3", ComponentIDs: []int64{1, 2}}
	in := input("50", disposal)
	in.Services[1], in.Services[2] = cold, hot
	in.Meters = []Meter{
		meter(1, "cold_water", 1, rd(sep, "10"), rd(oct, "11")), // 1 м³
		meter(2, "hot_water", 1, rd(sep, "5"), rd(oct, "7")),    // 2 м³
	}
	// Водоотведение: 1 + 2 = 3 м³ × свой тариф 30.5 = 91.5; слагаемые сами не назначены счёту и в строках не появляются.
	l := only(t, Compute(in))
	if l.Status != StatusOK || f(l.Quantity) != 3 || f(l.Amount) != 91.5 || len(l.Meters) != 2 {
		t.Errorf("sum_volumes: %+v", l)
	}
	// Сумма цен: 1×40 + 2×10 = 60; количества разные, поэтому ни количества, ни цены не показываем.
	in = input("50", both)
	in.Services[1], in.Services[2] = cold, hot
	in.Meters = []Meter{meter(1, "cold_water", 1, rd(sep, "10"), rd(oct, "11")), meter(2, "hot_water", 1, rd(sep, "5"), rd(oct, "7"))}
	l = only(t, Compute(in))
	if l.Status != StatusOK || f(l.Amount) != 60 || l.Quantity != nil || l.Price != nil {
		t.Errorf("sum_prices: %+v", l)
	}
	// Одинаковые количества: показываем количество и сумму цен.
	in.Meters = []Meter{meter(1, "cold_water", 1, rd(sep, "10"), rd(oct, "12")), meter(2, "hot_water", 1, rd(sep, "5"), rd(oct, "7"))}
	if l = only(t, Compute(in)); f(l.Quantity) != 2 || f(l.Price) != 50 || f(l.Amount) != 100 {
		t.Errorf("sum_prices equal: %+v", l)
	}
	// Слагаемое не посчиталось (нет показаний) — вся строка не считается, указано какое.
	in.Meters = []Meter{meter(1, "cold_water", 1, rd(sep, "10"), nil), meter(2, "hot_water", 1, rd(sep, "5"), rd(oct, "7"))}
	if l = only(t, Compute(in)); l.Status != StatusComponentFailed || l.Failed != "ХВС" {
		t.Errorf("component failed: %+v", l)
	}
	// У слагаемого нет цены.
	hot.PriceFirst = nil
	in.Meters = []Meter{meter(1, "cold_water", 1, rd(sep, "10"), rd(oct, "12")), meter(2, "hot_water", 1, rd(sep, "5"), rd(oct, "7"))}
	if l = only(t, Compute(in)); l.Status != StatusNoPrice || l.Failed != "ХВС для ГВС" {
		t.Errorf("component no price: %+v", l)
	}
}

func TestRoundRat(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"1.005", 2, "1.01"}, {"1.004999", 2, "1"}, {"2.5", 0, "3"}, {"0.00005", 4, "0.0001"}, {"0", 2, "0"}, {"-1.005", 2, "-1.01"}, {"10", 2, "10"},
	} {
		if got := roundRat(r(c.in), c.n); got.Cmp(r(c.want)) != 0 {
			t.Errorf("round(%s, %d) = %s, want %s", c.in, c.n, got.FloatString(6), c.want)
		}
	}
}

func TestTotalIsSumOfRoundedLines(t *testing.T) {
	// Три строки по 0.005 × 1 = 0.005 -> по 0.01 каждая; итог 0.03, а не округлённые 0.015.
	var services []*Service
	for i := int64(1); i <= 3; i++ {
		services = append(services, &Service{ID: i, Name: "x", Method: "manual", PriceFirst: r("0.005")})
	}
	in := input("1", services...)
	for i := range in.Assignments {
		in.Assignments[i].Quantity = r("1")
	}
	if res := Compute(in); res.Total != 0.03 {
		t.Errorf("total: %v", res.Total)
	}
}
