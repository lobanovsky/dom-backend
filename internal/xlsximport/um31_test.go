package xlsximport

import "testing"

func TestParseUM31DateWorkbookFormat(t *testing.T) {
	date, err := parseUM31Date("2026-11-30 23:34:46")
	if err != nil {
		t.Fatal(err)
	}
	if got := date.Time.Format("2006-01-02 15:04:05 -07:00"); got != "2026-11-30 23:34:46 +03:00" {
		t.Errorf("unexpected date: %s", got)
	}
}
