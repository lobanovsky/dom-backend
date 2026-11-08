package model

import "time"

func (s ResidentSchedule) Validate() error {
	if s.OpensDay < 1 || s.OpensDay > 31 || s.ClosesDay < s.OpensDay || s.ClosesDay > 31 {
		return invalid("opens_day", "expected days 1–31, opening no later than closing")
	}
	return nil
}

func (w ResidentWindow) IsOpen(day Date) bool {
	return w.Enabled && !day.Before(w.OpensOn.Time) && !day.After(w.ClosesOn.Time)
}

func ResidentWindowForMonth(s ResidentSchedule, overrides []ResidentWindow, period Date) ResidentWindow {
	period = period.FirstOfMonth()
	for _, w := range overrides {
		if w.Period.Equal(period.Time) {
			return w
		}
	}
	last := time.Date(period.Year(), period.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return ResidentWindow{BuildingID: s.BuildingID, Period: period, Enabled: s.Enabled,
		OpensOn:  NewDate(period.Year(), period.Month(), min(s.OpensDay, last)),
		ClosesOn: NewDate(period.Year(), period.Month(), min(s.ClosesDay, last))}
}

// ResolveResidentWindow вычисляет текущий месяц и следующее открытие без фонового cron.
func ResolveResidentWindow(s ResidentSchedule, overrides []ResidentWindow, today Date) ResidentWindow {
	w := ResidentWindowForMonth(s, overrides, today.FirstOfMonth())
	w.ServerDate = today
	if w.Enabled && !today.After(w.OpensOn.Time) {
		d := w.OpensOn
		w.NextOpensOn = &d
		return w
	}
	if s.Enabled {
		// Каждый пропущенный месяц требует отдельного отключающего исключения.
		for i := 1; i <= len(overrides)+1; i++ {
			p := Date{Time: today.FirstOfMonth().AddDate(0, i, 0)}
			next := ResidentWindowForMonth(s, overrides, p)
			if next.Enabled {
				d := next.OpensOn
				w.NextOpensOn = &d
				break
			}
		}
	} else {
		for _, next := range overrides {
			if next.Enabled && !next.OpensOn.Before(today.Time) && (w.NextOpensOn == nil || next.OpensOn.Before(w.NextOpensOn.Time)) {
				d := next.OpensOn
				w.NextOpensOn = &d
			}
		}
	}
	return w
}

func (in ResidentReadingInput) Reading() (MeterReading, error) {
	if in.T1 == nil {
		return MeterReading{}, invalid("t1", "is required")
	}
	if in.Comment != nil && len([]rune(*in.Comment)) > 2000 {
		return MeterReading{}, invalid("comment", "too long")
	}
	if in.Period.IsZero() || in.Period.Day() != 1 {
		return MeterReading{}, invalid("period", "expected first day of month")
	}
	r := MeterReading{MeterID: in.MeterID, Period: in.Period, T1: *in.T1, T2: in.T2, T3: in.T3, Comment: in.Comment, Source: "resident", ReadingDate: NewDate(2000, 1, 1)}
	return r, r.Validate()
}
