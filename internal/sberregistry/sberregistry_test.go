package sberregistry

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

// Синтетические строки: реальные реестры содержат персональные данные и в репозиторий не попадают.
const sample = "03-01-2026;09-32-33;1111;1111999V;100000000001;0000001001;ИВАНОВ ИВАН ИВАНОВИЧ;Г МОСКВА, КВ.1;1225;8575,32;8575,32;0,00;5\r\n" +
	"04-01-2026;19-46-47;1111;1111999V;100000000002;0000003001;ПЕТРОВ ПЕТР;Г МОСКВА, МАШИНОМЕСТО.1;1225;2231,21;2231,21;0,00;1\r\n" +
	"=2;10806,53;10806,53;0,00;686336;06-01-2026\r\n"

func cp1251(t *testing.T, s string) []byte {
	t.Helper()
	b, err := charmap.Windows1251.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParse(t *testing.T) {
	reg, errs, err := Parse("900005_9715357654_40703810338000004376_640.txt", cp1251(t, sample))
	if err != nil || len(errs) != 0 {
		t.Fatalf("err = %v, errs = %v", err, errs)
	}
	if reg.FileAccount != "40703810338000004376" || reg.RegistryNumber != "686336" || reg.RegistryDate.Format("2006-01-02") != "2026-01-06" {
		t.Errorf("header: %+v", reg)
	}
	if len(reg.Payments) != 2 || reg.TotalAmount != 1080653 {
		t.Fatalf("payments = %d, total = %d", len(reg.Payments), reg.TotalAmount)
	}
	p := reg.Payments[0]
	if p.ExternalID != "100000000001" || p.AccountNum != "0000001001" || p.PayerName != "ИВАНОВ ИВАН ИВАНОВИЧ" ||
		p.Amount != 857532 || p.Time != "09:32:33" || p.Date.Format("2006-01-02") != "2026-01-03" || p.Line != 1 {
		t.Errorf("payment 0 = %+v", p)
	}
}

func TestParseUTF8(t *testing.T) {
	if _, _, err := Parse("x.txt", []byte(sample)); err != nil {
		t.Fatalf("utf-8 input: %v", err)
	}
}

func TestParseRejects(t *testing.T) {
	for name, c := range map[string]struct{ in, want string }{
		"no summary":      {strings.Split(sample, "=")[0], "summary line"},
		"wrong count":     {strings.Replace(sample, "=2;", "=3;", 1), "payments"},
		"wrong total":     {strings.Replace(sample, "10806,53;10806,53", "10806,54;10806,53", 1), "does not match"},
		"empty":           {"=0;0,00;0,00;0,00;1;01-01-2026\r\n", "no payments"},
		"data after tail": {sample + "03-01-2026;09-32-33;1;1;3;4;A;B;1225;1,00;1,00;0,00;5\r\n", "after the summary"},
	} {
		if _, errs, err := Parse("x.txt", cp1251(t, c.in)); err == nil || len(errs) > 0 || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, errs = %v, want containing %q", name, err, errs, c.want)
		}
	}
}

func TestParseRowErrors(t *testing.T) {
	bad := strings.Replace(sample, "03-01-2026", "2026-01-03", 1)
	bad = strings.Replace(bad, "2231,21;2231,21", "2231,2123;2231,21", 1)
	_, errs, err := Parse("x.txt", cp1251(t, bad))
	if err != nil || len(errs) != 2 || errs[0].Row != 1 || errs[1].Row != 2 {
		t.Fatalf("errs = %v, err = %v", errs, err)
	}
}

func TestKopecks(t *testing.T) {
	for in, want := range map[string]int64{"8575,32": 857532, "0,00": 0, "12": 1200, "3,5": 350, "0.07": 7} {
		if got, err := kopecks(in); err != nil || got != want {
			t.Errorf("kopecks(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "1,234", "-1,00"} {
		if _, err := kopecks(in); err == nil {
			t.Errorf("kopecks(%q): expected error", in)
		}
	}
}

func TestFileAccount(t *testing.T) {
	for in, want := range map[string]string{
		"900005_9715357654_40703810338000004376_640.txt": "40703810338000004376",
		"9715357654_40705810238000000478_399.txt":        "40705810238000000478",
		"registry.txt": "",
	} {
		if got := FileAccount(in); got != want {
			t.Errorf("FileAccount(%q) = %q, want %q", in, got, want)
		}
	}
}
