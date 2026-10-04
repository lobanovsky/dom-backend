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
