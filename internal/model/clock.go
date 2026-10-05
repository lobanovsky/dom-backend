package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

const clockLayout = "15:04:05"

// Clock — время суток без даты; в JSON и БД это "15:04:05".
type Clock string

func (c *Clock) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("time must be a string HH:MM:SS")
	}
	// Браузерное поле time отдаёт «09:30» без секунд, если они нулевые.
	t, err := time.Parse(clockLayout, s)
	if err != nil {
		if t, err = time.Parse("15:04", s); err != nil {
			return fmt.Errorf("time must be HH:MM:SS")
		}
	}
	*c = Clock(t.Format(clockLayout))
	return nil
}

func (c *Clock) Scan(v any) error {
	switch x := v.(type) {
	case time.Time:
		*c = Clock(x.Format(clockLayout))
	case string:
		t, err := time.Parse(clockLayout, x)
		if err != nil {
			return fmt.Errorf("cannot parse time %q: %w", x, err)
		}
		*c = Clock(t.Format(clockLayout))
	default:
		return fmt.Errorf("cannot scan %T into Clock", v)
	}
	return nil
}

// Value отдаёт строку: PostgreSQL разбирает её как значение типа time.
func (c Clock) Value() (driver.Value, error) { return string(c), nil }
