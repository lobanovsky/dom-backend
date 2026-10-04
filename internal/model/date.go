package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

const dateLayout = "2006-01-02"

// Date — календарная дата без времени; в JSON и БД это "2006-01-02".
type Date struct{ time.Time }

func NewDate(y int, m time.Month, d int) Date {
	return Date{time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.Format(dateLayout)) }

func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("date must be a string YYYY-MM-DD")
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return fmt.Errorf("date must be YYYY-MM-DD")
	}
	d.Time = t
	return nil
}

func (d *Date) Scan(v any) error {
	t, ok := v.(time.Time)
	if !ok {
		return fmt.Errorf("cannot scan %T into Date", v)
	}
	d.Time = t
	return nil
}

func (d Date) Value() (driver.Value, error) { return d.Time, nil }
