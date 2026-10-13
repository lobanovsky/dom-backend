package sberexport

import (
	"testing"
	"time"

	"dom-backend/internal/sberregistry"
)

func TestBuildRoundTrip(t *testing.T) {
	reg := Registry{
		OrgINN: "9715357654", BankAccount: "40703810338000004376", Number: 7, Date: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		Rows: []Row{
			{ID: 15, Date: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), Amount: 857532, PayerName: "ИВАНОВ; ИВАН\nИВАНОВИЧ", AccountNumber: "0000001001", Address: "Г МОСКВА, КВ.1"},
			{ID: 16, Date: time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC), Time: "19:46:47", Amount: 5, PayerName: "ПЕТРОВ", AccountNumber: "0000003001"},
		},
	}
	data, err := reg.Build()
	if err != nil {
		t.Fatal(err)
	}
	name := reg.FileName()
	if name != "9715357654_40703810338000004376_7.txt" || sberregistry.FileAccount(name) != reg.BankAccount {
		t.Fatalf("file name = %q", name)
	}
	got, errs, err := sberregistry.Parse(name, data)
	if err != nil || len(errs) != 0 {
		t.Fatalf("parse: err = %v, errs = %v", err, errs)
	}
	if got.RegistryNumber != "7" || got.RegistryDate.Format("2006-01-02") != "2026-08-20" || len(got.Payments) != 2 || got.TotalAmount != 857537 {
		t.Fatalf("header: %+v", got)
	}
	p := got.Payments[0]
	if p.ExternalID != "000000000015" || p.AccountNum != "0000001001" || p.PayerName != "ИВАНОВ ИВАН ИВАНОВИЧ" || p.Amount != 857532 ||
		p.Time != "00:00:00" || p.Transferred != 857532 || p.Commission != 0 {
		t.Errorf("payment 0 = %+v", p)
	}
	if got.Payments[1].Time != "19:46:47" || got.Payments[1].Amount != 5 {
		t.Errorf("payment 1 = %+v", got.Payments[1])
	}
}
