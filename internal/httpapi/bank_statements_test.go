package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
	"golang.org/x/crypto/bcrypt"

	"dom-backend/internal/auth"
	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

const stmtAcct = "40703810338000009999"

// statementXLSX собирает минимальную синтетическую выписку: одно поступление и один итог.
func statementXLSX(t *testing.T, doc string) []byte {
	t.Helper()
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", stmtAcct)
	statementSheet(f, stmtAcct, stmtAcct, doc)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// statementSheet заполняет лист выписки по счёту account.
func statementSheet(f *excelize.File, sheet, account, doc string) {
	set := func(cell string, v any) { _ = f.SetCellValue(sheet, cell, v) }
	set("M5", account)
	for cell, v := range map[string]string{"B10": "Дата проводки", "E10": "Счет", "J10": "Сумма по дебету", "N10": "Сумма по кредиту",
		"O10": "№ документа", "Q10": "ВО", "R10": "Банк (БИК и наименование)", "U10": "Назначение платежа", "E11": "Дебет", "I11": "Кредит",
		"B15": "б/с", "H15": "Дебет", "L15": "Кредит", "B17": "Количество операций", "B19": "Итого оборотов"} {
		set(cell, v)
	}
	set("B12", time.Date(2026, 1, 5, 4, 31, 38, 0, time.UTC))
	set("E12", "40817810100044521912\n504908996115\nИВАНОВ ИВАН")
	set("I12", account+"\n9715357654\nТСН")
	set("N12", 100.5)
	set("O12", doc)
	set("Q12", "01")
	set("R12", "БИК 044525974 Банк")
	set("U12", "ЛС 0000001101")
	set("H17", "0")
	set("L17", "1")
	set("H19", "0,00")
	set("L19", "100.50")
}

type fakeStatements struct {
	BankStatementStore
	imported []string
}

func (f *fakeStatements) Import(_ context.Context, name string, _ []byte, st *model.ParsedStatement) (model.StatementImportResult, error) {
	switch name {
	case "dup.xlsx":
		return model.StatementImportResult{}, &store.StatementExistsError{StatementID: 5, FileName: "stored.xlsx"}
	case "all.xlsx":
		return model.StatementImportResult{}, &store.StatementAllDuplicatesError{Total: 1, Skipped: []model.SkippedOperation{{Counterpart: "X"}}}
	case "unknown.xlsx":
		return model.StatementImportResult{}, &store.UnknownBankAccountError{Number: st.Account}
	}
	f.imported = append(f.imported, name)
	return model.StatementImportResult{StatementID: 9, Incoming: 1}, nil
}

func postStatements(t *testing.T, fake *fakeStatements, files ...upload) (int, map[string]any) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	h := NewRouter(Deps{Auth: auth.New("admin", string(hash), "secret", time.Hour), BankStatements: fake})
	cookie := login(t, h)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, f := range files {
		part, _ := mw.CreateFormFile("file", f.name)
		_, _ = part.Write([]byte(f.body))
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bank-statements/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestImportStatementsZipAndStatuses(t *testing.T) {
	x := string(statementXLSX(t, "1"))
	archive := zipOf(t, map[string]string{
		"2026/январь/Выписка " + stmtAcct + ".xlsx": x,
		"__MACOSX/2026/._Выписка.xlsx":              "junk",
		"2026/~$lock.xlsx":                          "temp",
		"2026/notes.txt":                            "ignored",
		"2026/dup.xlsx":                             x,
		"2026/all.xlsx":                             x,
		"2026/unknown.xlsx":                         x,
		"2026/broken.xlsx":                          "not an xlsx",
		"2026/wrong_40703810338000000000.xlsx":      x,
	})
	fake := &fakeStatements{}
	code, out := postStatements(t, fake, upload{"stmts.zip", string(archive)})
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, out)
	}
	got := statuses(out)
	want := map[string]string{
		"stmts.zip/2026/январь/Выписка " + stmtAcct + ".xlsx": "imported",
		"stmts.zip/2026/dup.xlsx":                        "duplicate_file",
		"stmts.zip/2026/all.xlsx":                        "all_duplicates",
		"stmts.zip/2026/unknown.xlsx":                    "unknown_account",
		"stmts.zip/2026/broken.xlsx":                     "invalid",
		"stmts.zip/2026/wrong_40703810338000000000.xlsx": "invalid",
	}
	if len(got) != len(want) {
		t.Fatalf("files = %v", got)
	}
	for name, st := range want {
		if got[name] != st {
			t.Errorf("%s: %q, want %q", name, got[name], st)
		}
	}
	if len(fake.imported) != 1 || fake.imported[0] != "Выписка "+stmtAcct+".xlsx" {
		t.Errorf("imported = %v", fake.imported)
	}
	s := out["summary"].(map[string]any)
	if s["files_imported"] != 1.0 || s["files_failed"] != 5.0 || s["files_ignored"] != 2.0 || s["incoming_created"] != 1.0 || s["operations_skipped"] != 1.0 {
		t.Errorf("summary = %v", s)
	}
}

func TestImportStatementsPlainFilesAndErrors(t *testing.T) {
	code, out := postStatements(t, &fakeStatements{}, upload{"a.xlsx", string(statementXLSX(t, "1"))}, upload{"b.pdf", "x"})
	if code != http.StatusOK || statuses(out)["a.xlsx"] != "imported" || len(statuses(out)) != 1 {
		t.Errorf("plain files: status = %d, out = %v", code, out)
	}
	if code, _ := postStatements(t, &fakeStatements{}); code != http.StatusBadRequest {
		t.Errorf("no files: status = %d, want 400", code)
	}
}

func TestImportStatementWithSeveralSheets(t *testing.T) {
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", stmtAcct)
	statementSheet(f, stmtAcct, stmtAcct, "1")
	const second = "40705810238000000478"
	if _, err := f.NewSheet(second); err != nil {
		t.Fatal(err)
	}
	statementSheet(f, second, second, "2")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	fake := &fakeStatements{}
	code, out := postStatements(t, fake, upload{"2020.xlsx", buf.String()})
	got := statuses(out)
	if code != http.StatusOK || len(got) != 2 || got["2020.xlsx — лист "+stmtAcct] != "imported" || got["2020.xlsx — лист "+second] != "imported" {
		t.Fatalf("several sheets: status = %d, got = %v", code, got)
	}
	if len(fake.imported) != 2 || fake.imported[0] != "2020.xlsx — лист "+stmtAcct {
		t.Errorf("imported = %v", fake.imported)
	}
	if s := out["summary"].(map[string]any); s["files_imported"] != 2.0 || s["incoming_created"] != 2.0 {
		t.Errorf("summary = %v", s)
	}
}
