package model

import "testing"

func TestGateInput(t *testing.T) {
	valid := GateInput{PremisesID: 1, Zone: "courtyard", Phone: "8 (999) 123-45-67", RecipientName: " Анна ", Relation: "relative", VehiclePlate: " а123аа77 "}
	valid.Normalize()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if valid.Phone != "79991234567" || valid.RecipientName != "Анна" || valid.VehiclePlate != "А123АА77" {
		t.Fatalf("normalization: %+v", valid)
	}
	for _, bad := range []func(*GateInput){func(v *GateInput) { v.Phone = "123" }, func(v *GateInput) { v.Phone = "79991234567;admin" }, func(v *GateInput) { v.Zone = "all" }, func(v *GateInput) { v.PremisesID = 0 }, func(v *GateInput) { v.Relation = "admin" }, func(v *GateInput) { v.RecipientName = "" }} {
		v := valid
		bad(&v)
		if v.Validate() == nil {
			t.Fatalf("invalid accepted: %+v", v)
		}
	}
	if err := ValidateRole(RoleGuard); err != nil {
		t.Fatal(err)
	}
}
