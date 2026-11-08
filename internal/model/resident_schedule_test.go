package model

import (
	"testing"
	"time"
)

func TestResidentScheduleCalendar(t *testing.T) {
	s := ResidentSchedule{BuildingID: 1, Enabled: true, OpensDay: 17, ClosesDay: 22}
	for _, tc := range []struct {
		day  int
		open bool
		next string
	}{{16, false, "2026-11-17"}, {17, true, "2026-11-17"}, {22, true, "2026-12-17"}, {23, false, "2026-12-17"}} {
		w := ResolveResidentWindow(s, nil, NewDate(2026, 11, tc.day))
		if w.IsOpen(w.ServerDate) != tc.open || w.NextOpensOn == nil || w.NextOpensOn.Format("2006-01-02") != tc.next {
			t.Fatalf("day %d: %+v", tc.day, w)
		}
	}
	w := ResolveResidentWindow(s, nil, NewDate(2026, 12, 31))
	if w.NextOpensOn == nil || w.NextOpensOn.Format("2006-01-02") != "2027-01-17" {
		t.Fatal("year rollover", w)
	}
	for _, year := range []int{2027, 2028} {
		leap := ResidentSchedule{BuildingID: 1, Enabled: true, OpensDay: 30, ClosesDay: 31}
		w := ResolveResidentWindow(leap, nil, NewDate(year, time.February, 10))
		expected := 28
		if year == 2028 {
			expected = 29
		}
		if w.OpensOn.Day() != expected || w.ClosesOn.Day() != expected {
			t.Fatal("short month", w)
		}
	}
}
func TestResidentScheduleExceptions(t *testing.T) {
	s := ResidentSchedule{BuildingID: 1, Enabled: true, OpensDay: 17, ClosesDay: 22}
	overrides := []ResidentWindow{{BuildingID: 1, Period: NewDate(2026, 11, 1), Enabled: false, OpensOn: NewDate(2026, 11, 17), ClosesOn: NewDate(2026, 11, 22)}, {BuildingID: 1, Period: NewDate(2026, 12, 1), Enabled: false, OpensOn: NewDate(2026, 12, 17), ClosesOn: NewDate(2026, 12, 22)}}
	w := ResolveResidentWindow(s, overrides, NewDate(2026, 11, 20))
	if w.Enabled || w.NextOpensOn == nil || w.NextOpensOn.Format("2006-01-02") != "2027-01-17" {
		t.Fatal("disabled exceptions", w)
	}
	s.Enabled = false
	future := ResidentWindow{BuildingID: 1, Period: NewDate(2028, 3, 1), Enabled: true, OpensOn: NewDate(2028, 3, 15), ClosesOn: NewDate(2028, 3, 25)}
	w = ResolveResidentWindow(s, []ResidentWindow{future}, NewDate(2026, 11, 20))
	if w.NextOpensOn == nil || !w.NextOpensOn.Equal(future.OpensOn.Time) {
		t.Fatal("manual future opening", w)
	}
	for _, s := range []ResidentSchedule{{OpensDay: 0, ClosesDay: 22}, {OpensDay: 23, ClosesDay: 22}, {OpensDay: 17, ClosesDay: 32}} {
		if s.Validate() == nil {
			t.Fatal("invalid days accepted")
		}
	}
}
