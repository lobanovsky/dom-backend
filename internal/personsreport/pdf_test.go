package personsreport

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"dom-backend/internal/model"
)

func area(v float64) *float64 { return &v }

func sample(n int, multi bool) model.PersonsReport {
	rep := model.PersonsReport{Day: "2026-11-07", MultipleBuildings: multi}
	for i := 1; i <= n; i++ {
		p := model.PersonsReportPerson{Name: fmt.Sprintf("Тестов Человек %d Отчествович", i), Phones: []string{"+7 (903) 111-22-33", "+7 (926) 444-55-66"}, Emails: []string{"test@example.com"}}
		if i%3 == 0 {
			p.Phones, p.Emails = nil, nil
		}
		p.Items = append(p.Items, model.PersonsReportItem{Label: fmt.Sprintf("кв.%03d", i), Address: "17-й проезд Марьиной Рощи, 1", Area: area(50.5), ShareNum: 1, ShareDen: 2, Cadastral: "77:09:0001001:1234"})
		for j := 0; j < i%4; j++ {
			p.Items = append(p.Items, model.PersonsReportItem{Label: fmt.Sprintf("мм.%03d", i*3+j), Address: "17-й проезд Марьиной Рощи, 1", Area: area(13.2), ShareNum: 1, ShareDen: 1})
		}
		rep.Persons = append(rep.Persons, p)
	}
	return rep
}

func TestRenderPersonsReport(t *testing.T) {
	data, err := Render(sample(80, false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "%PDF-") {
		t.Fatalf("not a PDF: %q", data[:8])
	}
	if pages := len(regexp.MustCompile(`/Type /Page[^s]`).FindAll(data, -1)); pages < 3 {
		t.Errorf("80 people must span several pages, got %d", pages)
	}
	if _, err := Render(sample(5, true)); err != nil {
		t.Fatalf("several buildings: %v", err)
	}
	if _, err := Render(model.PersonsReport{Day: "2026-11-07"}); err != nil {
		t.Fatalf("empty report: %v", err)
	}
	// Для ручного просмотра: PERSONS_REPORT_SAMPLE=/path/file.pdf go test ./internal/personsreport
	if out := os.Getenv("PERSONS_REPORT_SAMPLE"); out != "" {
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFormatPhone(t *testing.T) {
	for _, raw := range []string{"+79636110726", "89636110726", "9636110726", "7 963 611-07-26", "+7 (963) 611-07-26", "8 (963) 611 07 26"} {
		if got := formatPhone(raw); got != "+7 (963) 611-07-26" {
			t.Errorf("formatPhone(%q) = %q", raw, got)
		}
	}
	for _, raw := range []string{"+1 415 555 2671", "+442071838750", "+7 (495) 123-45-67 доб. 12", "12345", ""} {
		if got := formatPhone(raw); got != raw {
			t.Errorf("formatPhone(%q) = %q, want unchanged", raw, got)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	for in, want := range map[float64]string{97.2: "97,2", 97: "97", 13.25: "13,25"} {
		if got := formatNumber(in); got != want {
			t.Errorf("formatNumber(%v) = %q, want %q", in, got, want)
		}
	}
}
