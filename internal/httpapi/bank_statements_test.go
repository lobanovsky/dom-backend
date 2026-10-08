package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"dom-backend/internal/auth"
	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

const (
	stmtAcct   = "40703810338000009999"
	stmtSecond = "40705810238000000478"
)

// oneC собирает синтетический файл обмена 1С (формат 1.03): по одному поступлению на каждый из счетов.
// Данные вымышленные: реальные выписки содержат персональные данные и в репозиторий не попадают.
func oneC(accounts ...string) string {
	var b strings.Builder
	b.WriteString("1CClientBankExchange\nВерсияФормата=1.03\nКодировка=UTF-8\nОтправитель=Тест\nПолучатель=\nДатаСоздания=08.10.2026\nВремяСоздания=10:00:00\nДатаНачала=05.01.2026\nДатаКонца=05.01.2026\n")
	for _, a := range accounts {
		b.WriteString("РасчСчет=" + a + "\n")
	}
	for _, a := range accounts {
		fmt.Fprintf(&b, "СекцияРасчСчет\nДатаНачала=05.01.2026\nДатаКонца=05.01.2026\nНачальныйОстаток=1000\nРасчСчет=%s\nВсегоСписано=0\nВсегоПоступило=100.5\nКонечныйОстаток=1100.5\nКонецРасчСчет\n", a)
	}
	for i, a := range accounts {
		fmt.Fprintf(&b, "СекцияДокумент=Платежное поручение\nНомер=%d\nДата=05.01.2026\nСумма=100.5\nПлательщикСчет=40817810100044521912\nДатаСписано=\nПлательщик=ИВАНОВ ИВАН\nПлательщикИНН=504908996115\nПлательщикРасчСчет=40817810100044521912\nПлательщикБИК=044525974\nПлательщикБанк1=Банк\nПолучательСчет=%s\nДатаПоступило=05.01.2026\nПолучатель=ТСН\nПолучательИНН=9715357654\nПолучательРасчСчет=%s\nВидОплаты=01\nНазначениеПлатежа=ЛС 0000001101\nКонецДокумента\n", i+1, a, a)
	}
	b.WriteString("КонецФайла\n")
	return b.String()
}

type fakeStatements struct {
	BankStatementStore
	imported []string
}

func (f *fakeStatements) Import(_ context.Context, name string, _ []byte, st *model.ParsedStatement) (model.StatementImportResult, error) {
	switch {
	case strings.HasPrefix(name, "dup"):
		return model.StatementImportResult{}, &store.StatementExistsError{StatementID: 5, FileName: "stored.txt"}
	case strings.HasPrefix(name, "all"):
		return model.StatementImportResult{}, &store.StatementAllDuplicatesError{Total: 1, Skipped: []model.SkippedOperation{{Counterpart: "X"}}}
	case strings.HasPrefix(name, "unknown"):
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
	good := oneC(stmtAcct)
	archive := zipOf(t, map[string]string{
		"2026/январь/kl_to_1c-2026.txt":     good,
		"__MACOSX/2026/._kl_to_1c-2026.txt": "junk",
		"2026/readme.txt":                   "это не выписка",
		"2026/dup.txt":                      good,
		"2026/all.txt":                      good,
		"2026/unknown.txt":                  good,
		"2026/broken.txt":                   "1CClientBankExchange\nВерсияФормата=1.03\nКодировка=UTF-8\nКонецФайла\n",
		"2026/old.xlsx":                     "Excel больше не поддерживается",
		"2026/wrongsum.txt":                 strings.Replace(good, "ВсегоПоступило=100.5", "ВсегоПоступило=999", 1),
	})
	fake := &fakeStatements{}
	code, out := postStatements(t, fake, upload{"stmts.zip", string(archive)})
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, out)
	}
	got := statuses(out)
	want := map[string]string{
		"stmts.zip/2026/январь/kl_to_1c-2026.txt": "imported",
		"stmts.zip/2026/dup.txt":                  "duplicate_file",
		"stmts.zip/2026/all.txt":                  "all_duplicates",
		"stmts.zip/2026/unknown.txt":              "unknown_account",
		"stmts.zip/2026/broken.txt":               "invalid",
		"stmts.zip/2026/wrongsum.txt":             "invalid",
	}
	if len(got) != len(want) {
		t.Fatalf("files = %v", got)
	}
	for name, st := range want {
		if got[name] != st {
			t.Errorf("%s: %q, want %q", name, got[name], st)
		}
	}
	if len(fake.imported) != 1 || fake.imported[0] != "kl_to_1c-2026.txt" {
		t.Errorf("imported = %v", fake.imported)
	}
	// readme.txt (не 1С) и .xlsx (формат больше не поддерживается) игнорируются
	s := out["summary"].(map[string]any)
	if s["files_imported"] != 1.0 || s["files_failed"] != 5.0 || s["files_ignored"] != 2.0 || s["incoming_created"] != 1.0 || s["operations_skipped"] != 1.0 {
		t.Errorf("summary = %v", s)
	}
}

func TestImportStatementsPlainFilesAndErrors(t *testing.T) {
	code, out := postStatements(t, &fakeStatements{}, upload{"a.txt", oneC(stmtAcct)}, upload{"b.pdf", "x"}, upload{"c.txt", "просто текст"})
	if code != http.StatusOK || statuses(out)["a.txt"] != "imported" || len(statuses(out)) != 1 {
		t.Errorf("plain files: status = %d, out = %v", code, out)
	}
	if s := out["summary"].(map[string]any); s["files_ignored"] != 2.0 {
		t.Errorf("summary = %v", s)
	}
	if code, _ := postStatements(t, &fakeStatements{}); code != http.StatusBadRequest {
		t.Errorf("no files: status = %d, want 400", code)
	}
}

// В одном файле 1С может быть несколько расчётных счетов: выписка на каждый.
func TestImportStatementWithSeveralAccounts(t *testing.T) {
	fake := &fakeStatements{}
	code, out := postStatements(t, fake, upload{"kl_to_1c.txt", oneC(stmtAcct, stmtSecond)})
	got := statuses(out)
	if code != http.StatusOK || len(got) != 2 || got["kl_to_1c.txt — счёт "+stmtAcct] != "imported" || got["kl_to_1c.txt — счёт "+stmtSecond] != "imported" {
		t.Fatalf("several accounts: status = %d, got = %v", code, got)
	}
	if len(fake.imported) != 2 {
		t.Errorf("imported = %v", fake.imported)
	}
	if s := out["summary"].(map[string]any); s["files_imported"] != 2.0 || s["incoming_created"] != 2.0 {
		t.Errorf("summary = %v", s)
	}
}

// Файлы выписок бывают в десятки мегабайт (1С за несколько лет), больше лимита одиночного реестра.
func TestStatementFileLargerThanRegistryLimit(t *testing.T) {
	big := oneC(stmtAcct) + strings.Repeat("#", maxRegistrySize+1024)
	code, out := postStatements(t, &fakeStatements{}, upload{"kl_to_1c.txt", big})
	if code != http.StatusOK || statuses(out)["kl_to_1c.txt"] != "imported" {
		t.Errorf("a statement file over the registry size limit must be accepted: status = %d, out = %v", code, out)
	}
}
