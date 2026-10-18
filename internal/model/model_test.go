package model

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func d(y int, m time.Month, day int) Date { return NewDate(y, m, day) }

func TestValidate(t *testing.T) {
	validOwnership := func() Ownership {
		return Ownership{PremisesID: 1, PersonID: ptr(int64(1)), ShareNum: 1, ShareDen: 2, ValidFrom: d(2020, 1, 1)}
	}
	cases := []struct {
		name  string
		err   error
		field string // пусто — ошибки быть не должно
	}{
		{"organization ok", Organization{Kind: "tsn", Name: "ТСН"}.Validate(), ""},
		{"organization bad kind", Organization{Kind: "x", Name: "ТСН"}.Validate(), "kind"},
		{"organization no name", Organization{Kind: "uk"}.Validate(), "name"},
		{"building bad year", Building{OrganizationID: 1, Kind: "parking", Address: "a", YearBuilt: ptr(1500)}.Validate(), "year_built"},
		{"premises living > total", Premises{BuildingID: 1, Kind: "apartment", Number: "1", TotalArea: ptr(40.0), LivingArea: ptr(50.0)}.Validate(), "living_area"},
		{"person no first name", Person{LastName: "Иванов"}.Validate(), "first_name"},
		{"person contacts ok", Person{LastName: "И", FirstName: "И", Phones: []string{"+7 (495) 123-45-67", "8 903 111 22 33 доб. 4"}, Emails: []string{"a@b.ru"}}.Validate(), ""},
		{"person phone too short", Person{LastName: "И", FirstName: "И", Phones: []string{"12-34"}}.Validate(), "phones"},
		{"person phone text", Person{LastName: "И", FirstName: "И", Phones: []string{"позвонить"}}.Validate(), "phones"},
		{"person bad email", Person{LastName: "И", FirstName: "И", Emails: []string{"не-email"}}.Validate(), "emails"},
		{"person email with name", Person{LastName: "И", FirstName: "И", Emails: []string{"Иван <a@b.ru>"}}.Validate(), "emails"},
		{"person too many phones", Person{LastName: "И", FirstName: "И", Phones: manyPhones(11)}.Validate(), "phones"},
		{"ownership ok", validOwnership().Validate(), ""},
		{"ownership share > 1", func() error { o := validOwnership(); o.ShareNum, o.ShareDen = 3, 2; return o.Validate() }(), "share_num"},
		{"ownership both owners", func() error { o := validOwnership(); o.LegalEntityID = ptr(int64(2)); return o.Validate() }(), "person_id"},
		{"ownership no owner", func() error { o := validOwnership(); o.PersonID = nil; return o.Validate() }(), "person_id"},
		{"ownership bad period", func() error { o := validOwnership(); o.ValidTo = ptr(d(2019, 1, 1)); return o.Validate() }(), "valid_to"},
		{"ownership no start", func() error { o := validOwnership(); o.ValidFrom = Date{}; return o.Validate() }(), "valid_from"},
		{"residency bad relation", Residency{PersonID: 1, PremisesID: 1, Relation: "friend", ValidFrom: d(2020, 1, 1)}.Validate(), "relation"},
		{"account closed before opened", Account{Number: "1", PremisesID: 1, Purpose: "utilities", Status: "closed", OpenedAt: d(2020, 1, 1), ClosedAt: ptr(d(2019, 1, 1))}.Validate(), "closed_at"},
		{"holder ok", AccountHolder{AccountID: 1, LegalEntityID: ptr(int64(1)), ValidFrom: d(2020, 1, 1)}.Validate(), ""},
	}
	for _, tc := range cases {
		var ve *ValidationError
		switch {
		case tc.field == "" && tc.err != nil:
			t.Errorf("%s: unexpected error: %v", tc.name, tc.err)
		case tc.field != "" && !errors.As(tc.err, &ve):
			t.Errorf("%s: want ValidationError on %q, got %v", tc.name, tc.field, tc.err)
		case tc.field != "" && ve.Field != tc.field:
			t.Errorf("%s: field = %q, want %q", tc.name, ve.Field, tc.field)
		}
	}
}

func manyPhones(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "+7 900 000 00 " + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}
	return out
}

func TestPersonNormalize(t *testing.T) {
	p := Person{
		Phones: []string{" +7 900 111 22 33 ", "", "+7 900 111 22 33", "8 495 000 00 00"},
		Emails: nil,
	}
	p.Normalize()
	if len(p.Phones) != 2 || p.Phones[0] != "+7 900 111 22 33" || p.Phones[1] != "8 495 000 00 00" {
		t.Errorf("phones = %q, want trimmed, deduplicated, order kept", p.Phones)
	}
	if p.Emails == nil || len(p.Emails) != 0 {
		t.Errorf("emails = %#v, want empty non-nil slice", p.Emails)
	}
}

func TestDefaults(t *testing.T) {
	a := Account{}
	a.SetDefaults()
	if a.Purpose != "utilities" || a.Status != "active" {
		t.Errorf("account defaults = %+v", a)
	}
	o := Ownership{}
	o.SetDefaults()
	if o.ShareNum != 1 || o.ShareDen != 1 {
		t.Errorf("ownership defaults = %d/%d", o.ShareNum, o.ShareDen)
	}
}

func TestDateJSON(t *testing.T) {
	var p struct {
		From Date  `json:"from"`
		To   *Date `json:"to"`
	}
	if err := json.Unmarshal([]byte(`{"from":"2020-03-05","to":null}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.To != nil || p.From.Format("2006-01-02") != "2020-03-05" {
		t.Fatalf("parsed = %+v", p)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"from":"2020-03-05","to":null}` {
		t.Errorf("marshaled = %s", b)
	}
	for _, bad := range []string{`{"from":"05.03.2020"}`, `{"from":20200305}`} {
		if err := json.Unmarshal([]byte(bad), &p); err == nil {
			t.Errorf("%s: want error", bad)
		}
	}
}

func TestPaymentValidation(t *testing.T) {
	ok := IncomingPayment{BankAccountID: 1, PaymentDate: NewDate(2026, 1, 3), Amount: 100.5, PayerName: "ИВАНОВ"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid payment: %v", err)
	}
	noName := ok
	noName.PayerName = ""
	if err := noName.Validate(); err != nil {
		t.Errorf("payment without payer name must be valid: %v", err)
	}
	account, cat := int64(1), int64(2)
	for name, mutate := range map[string]func(*IncomingPayment){
		"zero amount":          func(p *IncomingPayment) { p.Amount = 0 },
		"fractional kopecks":   func(p *IncomingPayment) { p.Amount = 10.005 },
		"no date":              func(p *IncomingPayment) { p.PaymentDate = Date{} },
		"account and category": func(p *IncomingPayment) { p.PersonalAccountID, p.CategoryID = &account, &cat },
		"bad bik":              func(p *IncomingPayment) { b := "123"; p.PayerBIK = &b },
		"bad inn":              func(p *IncomingPayment) { b := "12345"; p.PayerINN = &b },
	} {
		p := ok
		mutate(&p)
		if p.Validate() == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestBankAccountValidation(t *testing.T) {
	ok := BankAccount{OrganizationID: 1, Number: "40703810338000004376", ValidFrom: NewDate(2020, 1, 1)}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	short := ok
	short.Number = "4070"
	to := NewDate(2019, 1, 1)
	backwards := ok
	backwards.ValidTo = &to
	if short.Validate() == nil || backwards.Validate() == nil {
		t.Error("expected errors for a short number and valid_to before valid_from")
	}
	withCodes := ok
	withCodes.RegistryCodes = []string{" VTB_231068_001 ", "", "vtb_231068_001", "VTB_224416"}
	withCodes.SetDefaults()
	if err := withCodes.Validate(); err != nil || len(withCodes.RegistryCodes) != 2 || withCodes.RegistryCodes[0] != "VTB_231068_001" {
		t.Errorf("codes after SetDefaults: %q, err = %v", withCodes.RegistryCodes, err)
	}
	withCodes.RegistryCodes = []string{"bad code"}
	if withCodes.Validate() == nil {
		t.Error("registry code with a space must be rejected")
	}
	withCodes.RegistryCodes = make([]string, maxRegistryCodes+1)
	for i := range withCodes.RegistryCodes {
		withCodes.RegistryCodes[i] = "C" + string(rune('A'+i))
	}
	if withCodes.Validate() == nil {
		t.Error("too many registry codes must be rejected")
	}
}

func TestPaymentCategoryStyleValidation(t *testing.T) {
	s := func(v string) *string { return &v }
	ok := PaymentCategory{Name: "Аренда", Direction: "incoming", Color: s("#1a2B3c"), Icon: s("🏠")}
	if err := ok.Validate(); err != nil {
		t.Errorf("valid category: %v", err)
	}
	if err := (PaymentCategory{Name: "Аренда", Direction: "incoming"}).Validate(); err != nil {
		t.Errorf("without style: %v", err)
	}
	for name, c := range map[string]PaymentCategory{
		"bad color":  {Name: "x", Direction: "incoming", Color: s("red")},
		"empty icon": {Name: "x", Direction: "incoming", Icon: s(" ")},
		"long icon":  {Name: "x", Direction: "incoming", Icon: s("12345678901234567")},
	} {
		if c.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestIncomingPaymentAttentionNeedsComment(t *testing.T) {
	base := IncomingPayment{BankAccountID: 1, PaymentDate: Date{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, Amount: 10}
	c := func(v string) *string { return &v }
	ok := base
	if err := ok.Validate(); err != nil {
		t.Errorf("without flag: %v", err)
	}
	for name, comment := range map[string]*string{"nil": nil, "empty": c(""), "spaces": c("  ")} {
		p := base
		p.Attention, p.Comment = true, comment
		if err := p.Validate(); err == nil {
			t.Errorf("attention with %s comment: want error", name)
		}
	}
	p := base
	p.Attention, p.Comment = true, c("капремонт на обычный счёт")
	if err := p.Validate(); err != nil {
		t.Errorf("attention with comment: %v", err)
	}
}

func TestMeterValidate(t *testing.T) {
	i64 := func(v int64) *int64 { return &v }
	day := func(d int) Date { return NewDate(2026, 1, d) }
	dp := func(d Date) *Date { return &d }
	base := Meter{PremisesID: i64(1), Resource: "electricity", Tariffs: 3, SerialNumber: "A1", Coefficient: 1, InstalledAt: day(10)}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid meter: %v", err)
	}
	common := base
	common.PremisesID, common.BuildingID = nil, i64(1)
	if err := common.Validate(); err != nil {
		t.Errorf("common meter: %v", err)
	}
	mut := func(f func(m *Meter)) Meter { m := base; f(&m); return m }
	for name, m := range map[string]Meter{
		"no scope":         mut(func(m *Meter) { m.PremisesID = nil }),
		"both scopes":      mut(func(m *Meter) { m.BuildingID = i64(1) }),
		"bad resource":     mut(func(m *Meter) { m.Resource = "gas" }),
		"water tariffs":    mut(func(m *Meter) { m.Resource, m.Tariffs = "cold_water", 2 }),
		"four tariffs":     mut(func(m *Meter) { m.Tariffs = 4 }),
		"no serial":        mut(func(m *Meter) { m.SerialNumber = " " }),
		"no install date":  mut(func(m *Meter) { m.InstalledAt = Date{} }),
		"removed before":   mut(func(m *Meter) { m.RemovedAt = dp(day(9)) }),
		"next before last": mut(func(m *Meter) { m.VerifiedAt, m.NextVerificationAt = dp(day(5)), dp(day(4)) }),
		"negative initial": mut(func(m *Meter) { m.InitialT1 = -1 }),
		"4 decimals":       mut(func(m *Meter) { m.InitialT1 = 1.2345 }),
		"zero coefficient": mut(func(m *Meter) { m.Coefficient = 0 }),
	} {
		if m.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
	d := Meter{PremisesID: i64(1), Resource: "cold_water", SerialNumber: "W", InstalledAt: day(1), InitialT2: 5}
	d.SetDefaults()
	if d.Tariffs != 1 || d.Coefficient != 1 || d.InitialT2 != 0 {
		t.Errorf("defaults: %+v", d)
	}
}

func TestMeterReadingValidate(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	r := MeterReading{MeterID: 1, ReadingDate: NewDate(2026, 2, 1), T1: 10.5, T2: f(3), Source: "manual"}
	if err := r.Validate(); err != nil {
		t.Fatalf("valid reading: %v", err)
	}
	for name, mut := range map[string]func(r *MeterReading){
		"no meter":     func(r *MeterReading) { r.MeterID = 0 },
		"no date":      func(r *MeterReading) { r.ReadingDate = Date{} },
		"negative":     func(r *MeterReading) { r.T1 = -0.001 },
		"4 decimals":   func(r *MeterReading) { r.T2 = f(1.0001) },
		"negative t3":  func(r *MeterReading) { r.T3 = f(-1) },
		"unknown kind": func(r *MeterReading) { r.Source = "photo" },
	} {
		c := r
		mut(&c)
		if c.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if Milli(10.123) != 10123 {
		t.Errorf("Milli(10.123) = %d", Milli(10.123))
	}
}

func TestServiceValidate(t *testing.T) {
	str := func(v string) *string { return &v }
	num := func(v int) *int { return &v }
	base := Service{OrganizationID: 1, Kind: "utility", Name: "Электроэнергия Т1", Unit: "kwh", CalcMethod: "individual_meter", MeterResource: str("electricity"), MeterTariff: num(2), AccountPurpose: "utilities", Active: true}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid service: %v", err)
	}
	mut := func(f func(s *Service)) Service { s := base; f(&s); return s }
	for name, s := range map[string]Service{
		"no organization":  mut(func(s *Service) { s.OrganizationID = 0 }),
		"bad kind":         mut(func(s *Service) { s.Kind = "other" }),
		"no name":          mut(func(s *Service) { s.Name = " " }),
		"bad unit":         mut(func(s *Service) { s.Unit = "kg" }),
		"bad method":       mut(func(s *Service) { s.CalcMethod = "magic" }),
		"no resource":      mut(func(s *Service) { s.MeterResource = nil }),
		"no meter tariff":  mut(func(s *Service) { s.MeterTariff = nil }),
		"tariff 4":         mut(func(s *Service) { s.MeterTariff = num(4) }),
		"water tariff 2":   mut(func(s *Service) { s.MeterResource = str("cold_water") }),
		"per area in kwh":  mut(func(s *Service) { s.CalcMethod, s.MeterResource, s.MeterTariff = "per_area", nil, nil }),
		"bad purpose":      mut(func(s *Service) { s.AccountPurpose = "rent" }),
		"bad kind in list": mut(func(s *Service) { s.PremisesKinds = []string{"apartment", "garage"} }),
		"area norm in m2": mut(func(s *Service) {
			s.CalcMethod, s.Unit, s.MeterResource, s.MeterTariff = "per_area_norm", "m2", nil, nil
		}),
		"sum no component":        mut(func(s *Service) { s.CalcMethod, s.MeterResource, s.MeterTariff = "sum_volumes", nil, nil }),
		"sum prices no component": mut(func(s *Service) { s.CalcMethod, s.MeterResource, s.MeterTariff = "sum_prices", nil, nil }),
	} {
		if s.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
	ok := mut(func(s *Service) { s.PremisesKinds = []string{"apartment", "parking_space"}; s.ReceiptName = str("  ") })
	ok.SetDefaults()
	if err := ok.Validate(); err != nil || ok.ReceiptName != nil {
		t.Errorf("kinds list: %v, receipt = %v", err, ok.ReceiptName)
	}
	def := Service{OrganizationID: 1, Kind: "additional", Name: "Ключи", Unit: "piece", CalcMethod: "manual"}
	def.SetDefaults()
	if def.AccountPurpose != "utilities" || def.PremisesKinds == nil || def.Validate() != nil {
		t.Errorf("defaults: %+v", def)
	}
	sum := Service{OrganizationID: 1, Kind: "utility", Name: "Водоотведение", Unit: "m3", CalcMethod: "sum_volumes", ComponentIDs: []int64{1, 2}, MeterResource: str("hot_water")}
	sum.SetDefaults()
	if sum.MeterResource != nil || sum.Validate() != nil {
		t.Errorf("sum service: %+v, %v", sum, sum.Validate())
	}
	fixed := Service{OrganizationID: 1, Kind: "additional", Name: "Консьерж", Unit: "month", CalcMethod: "fixed", ComponentIDs: []int64{5}}
	fixed.SetDefaults()
	if len(fixed.ComponentIDs) != 0 || fixed.ComponentIDs == nil || fixed.Validate() != nil {
		t.Errorf("fixed service: %+v", fixed)
	}
	water := Service{OrganizationID: 1, Kind: "utility", Name: "ХВС", Unit: "m3", CalcMethod: "individual_meter", MeterResource: str("cold_water")}
	water.SetDefaults()
	if water.MeterTariff == nil || *water.MeterTariff != 1 || water.Validate() != nil {
		t.Errorf("water defaults: %+v", water)
	}
}

func TestServiceNormValidate(t *testing.T) {
	dp := func(d Date) *Date { return &d }
	base := ServiceNorm{ServiceID: 1, BuildingID: 2, Value: 0.011, ValidFrom: NewDate(2026, 1, 1)}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid norm: %v", err)
	}
	mut := func(f func(n *ServiceNorm)) ServiceNorm { n := base; f(&n); return n }
	for name, n := range map[string]ServiceNorm{
		"no service":  mut(func(n *ServiceNorm) { n.ServiceID = 0 }),
		"no building": mut(func(n *ServiceNorm) { n.BuildingID = 0 }),
		"zero":        mut(func(n *ServiceNorm) { n.Value = 0 }),
		"negative":    mut(func(n *ServiceNorm) { n.Value = -0.01 }),
		"6 decimals":  mut(func(n *ServiceNorm) { n.Value = 0.011111 }),
		"no from":     mut(func(n *ServiceNorm) { n.ValidFrom = Date{} }),
		"to before":   mut(func(n *ServiceNorm) { n.ValidTo = dp(NewDate(2025, 12, 31)) }),
	} {
		if n.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if err := mut(func(n *ServiceNorm) { n.Value = 0.01625; n.ValidTo = dp(n.ValidFrom) }).Validate(); err != nil {
		t.Errorf("valid norm with 5 decimals: %v", err)
	}
}

func TestServiceTariffValidate(t *testing.T) {
	dp := func(d Date) *Date { return &d }
	base := ServiceTariff{ServiceID: 1, Price: 5.8913, ValidFrom: NewDate(2026, 1, 1)}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid tariff: %v", err)
	}
	mut := func(f func(t *ServiceTariff)) ServiceTariff { t := base; f(&t); return t }
	for name, tr := range map[string]ServiceTariff{
		"no service": mut(func(t *ServiceTariff) { t.ServiceID = 0 }),
		"negative":   mut(func(t *ServiceTariff) { t.Price = -1 }),
		"5 decimals": mut(func(t *ServiceTariff) { t.Price = 1.23456 }),
		"no from":    mut(func(t *ServiceTariff) { t.ValidFrom = Date{} }),
		"to before":  mut(func(t *ServiceTariff) { t.ValidTo = dp(NewDate(2025, 12, 31)) }),
	} {
		if tr.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if err := mut(func(t *ServiceTariff) { t.ValidTo = dp(t.ValidFrom) }).Validate(); err != nil {
		t.Errorf("one-day tariff: %v", err)
	}
}

func TestTariffValidate(t *testing.T) {
	dp := func(d Date) *Date { return &d }
	if err := (Tariff{OrganizationID: 1, Name: "Электроэнергия Т1", Unit: "kwh"}).Validate(); err != nil {
		t.Fatalf("valid tariff: %v", err)
	}
	for name, tr := range map[string]Tariff{
		"no org":   {Name: "x", Unit: "kwh"},
		"no name":  {OrganizationID: 1, Name: " ", Unit: "kwh"},
		"bad unit": {OrganizationID: 1, Name: "x", Unit: "kg"},
	} {
		if tr.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
	base := TariffPrice{TariffID: 1, Price: 6.1, ValidFrom: NewDate(2026, 1, 1)}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid price: %v", err)
	}
	for name, p := range map[string]TariffPrice{
		"no tariff":  {Price: 1, ValidFrom: NewDate(2026, 1, 1)},
		"negative":   {TariffID: 1, Price: -1, ValidFrom: NewDate(2026, 1, 1)},
		"5 decimals": {TariffID: 1, Price: 1.23456, ValidFrom: NewDate(2026, 1, 1)},
		"no from":    {TariffID: 1, Price: 1},
		"to before":  {TariffID: 1, Price: 1, ValidFrom: NewDate(2026, 1, 1), ValidTo: dp(NewDate(2025, 12, 31))},
	} {
		if p.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if (TariffFromServiceRequest{}).Validate() == nil {
		t.Errorf("request without service")
	}
	id := int64(3)
	linked := Service{OrganizationID: 1, Kind: "utility", Name: "Сумма", Unit: "m3", CalcMethod: "sum_prices", ComponentIDs: []int64{1}, AccountPurpose: "utilities", TariffID: &id}
	if linked.Validate() == nil {
		t.Errorf("sum_prices with tariff_id: want error")
	}
}
