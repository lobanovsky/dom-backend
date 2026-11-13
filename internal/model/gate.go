package model

import (
	"strings"
	"time"
	"unicode"
)

// GateInput — неизменяемые после отправки сведения получателя доступа.
type GateInput struct {
	PremisesID        int64  `json:"premises_id"`
	Zone              string `json:"zone"`
	Phone             string `json:"phone"`
	RecipientName     string `json:"recipient_name"`
	Relation          string `json:"relation"`
	VehiclePlate      string `json:"vehicle_plate"`
	VehicleBrand      string `json:"vehicle_brand"`
	OccasionalVehicle bool   `json:"occasional_vehicle"`
	ValidUntil        *Date  `json:"valid_until"`
	Comment           string `json:"comment"`
}

func (v *GateInput) Normalize() {
	v.Phone = NormalizePhone(v.Phone)
	v.RecipientName = strings.TrimSpace(v.RecipientName)
	v.VehiclePlate = strings.ToUpper(strings.TrimSpace(v.VehiclePlate))
	v.VehicleBrand = strings.TrimSpace(v.VehicleBrand)
	v.Comment = strings.TrimSpace(v.Comment)
}
func (v GateInput) Validate() error {
	if v.PremisesID <= 0 {
		return invalid("premises_id", "must be positive")
	}
	if err := oneOf("zone", v.Zone, []string{"courtyard", "parking"}); err != nil {
		return err
	}
	if len(v.Phone) < 11 || len(v.Phone) > 15 || strings.Trim(v.Phone, "0123456789") != "" || v.Phone[0] == '0' {
		return invalid("phone", "gate phone must contain 11 to 15 digits")
	}
	if err := oneOf("relation", v.Relation, []string{"owner", "relative", "tenant", "guest", "service"}); err != nil {
		return err
	}
	for _, f := range []struct {
		name, value string
		max         int
	}{{"recipient_name", v.RecipientName, 120}, {"vehicle_plate", v.VehiclePlate, 24}, {"vehicle_brand", v.VehicleBrand, 80}, {"comment", v.Comment, 500}} {
		if len([]rune(f.value)) > f.max || strings.IndexFunc(f.value, func(r rune) bool { return unicode.IsControl(r) && !(f.name == "comment" && (r == '\n' || r == '\t')) }) >= 0 {
			return invalid(f.name, "gate text is too long or contains control characters")
		}
	}
	if v.RecipientName == "" {
		return invalid("recipient_name", "is required")
	}
	return nil
}

type GateRequest struct {
	ID                int64     `json:"id" db:"id"`
	PersonID          int64     `json:"person_id" db:"person_id"`
	PremisesID        int64     `json:"premises_id" db:"premises_id"`
	BuildingID        int64     `json:"building_id" db:"building_id"`
	ApplicantName     string    `json:"applicant_name" db:"applicant_name"`
	Address           string    `json:"address" db:"address"`
	PremisesNumber    string    `json:"premises_number" db:"premises_number"`
	PremisesKind      string    `json:"premises_kind" db:"premises_kind"`
	Zone              string    `json:"zone" db:"zone"`
	Phone             string    `json:"phone" db:"phone"`
	RecipientName     string    `json:"recipient_name" db:"recipient_name"`
	Relation          string    `json:"relation" db:"relation"`
	VehiclePlate      string    `json:"vehicle_plate" db:"vehicle_plate"`
	VehicleBrand      string    `json:"vehicle_brand" db:"vehicle_brand"`
	OccasionalVehicle bool      `json:"occasional_vehicle" db:"occasional_vehicle"`
	ValidUntil        *Date     `json:"valid_until" db:"valid_until"`
	Comment           string    `json:"comment" db:"comment"`
	Status            string    `json:"status" db:"status"`
	EffectiveStatus   string    `json:"effective_status" db:"effective_status"`
	Eligible          bool      `json:"eligible" db:"eligible"`
	Active            bool      `json:"active" db:"active"`
	ReviewNote        string    `json:"review_note" db:"review_note"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time `json:"updated_at" db:"updated_at"`
}
type GateProperty struct {
	ID         int64  `json:"id" db:"id"`
	BuildingID int64  `json:"building_id" db:"building_id"`
	Number     string `json:"number" db:"number"`
	Kind       string `json:"kind" db:"kind"`
	Address    string `json:"address" db:"address"`
	Zone       string `json:"zone" db:"zone"`
}
type GateEvent struct {
	ID        int64     `json:"id" db:"id"`
	Action    string    `json:"action" db:"action"`
	ActorRole string    `json:"actor_role" db:"actor_role"`
	Note      string    `json:"note" db:"note"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}
type GateFilter struct{ Status, Zone, Q string }
