package ownersregistry

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"dom-backend/internal/model"
)

func area(v float64) *float64 { return &v }

func sample(n int) model.OwnersRegistry {
	reg := model.OwnersRegistry{Address: "17-й проезд Марьиной Рощи, 1", Day: "2026-11-06"}
	for i := 1; i <= n; i++ {
		row := model.OwnersRegistryRow{Label: fmt.Sprintf("кв.%03d", i), Area: area(50.5 + float64(i%7)), Owners: []string{"Иванов Иван Иванович"}, Cadastral: "77:09:0001001:1234"}
		switch i % 5 {
		case 0:
			row.Owners = []string{"Лобановский Евгений Владимирович", "Фомина Валерия Александровна", "Очень Длинная Фамилия Составная Имя Отчество Продолжение"}
		case 1:
			row.Owners = nil
		case 2:
			row.Area = nil
		}
		reg.Rows = append(reg.Rows, row)
	}
	return reg
}

func TestRenderProducesPDFWithPages(t *testing.T) {
	data, err := Render(sample(120))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "%PDF-") {
		t.Fatalf("not a PDF: %q", data[:8])
	}
	pages := len(regexp.MustCompile(`/Type /Page[^s]`).FindAll(data, -1))
	if pages < 3 {
		t.Errorf("120 rows must span several pages, got %d", pages)
	}
	// Для ручного просмотра: OWNERS_REGISTRY_SAMPLE=/path/file.pdf go test ./internal/ownersregistry
	if out := os.Getenv("OWNERS_REGISTRY_SAMPLE"); out != "" {
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenderEmptyRegistry(t *testing.T) {
	if _, err := Render(model.OwnersRegistry{Address: "x", Day: "2026-01-01"}); err != nil {
		t.Fatal(err)
	}
}

func TestFormatting(t *testing.T) {
	for in, want := range map[float64]string{97.2: "97,2", 97: "97", 56.55: "56,55", 11057: "11057"} {
		if got := formatNumber(in); got != want {
			t.Errorf("formatNumber(%v) = %q, want %q", in, got, want)
		}
	}
	if got := formatShare(97.2 / 11057 * 100); got != "0,88" {
		t.Errorf("formatShare = %q", got)
	}
	if got := formatDay("2026-11-06"); got != "06.11.2026" {
		t.Errorf("formatDay = %q", got)
	}
}
