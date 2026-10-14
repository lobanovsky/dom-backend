package httpapi

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

const (
	acct    = "40703810338000004376"
	otherAc = "40705810238000000478"
)

// Синтетические данные: реальные реестры содержат персональные данные.
const registryText = "03-01-2026;09-32-33;1111;1111999V;900000000001;0000001001;ИВАНОВ ИВАН;Г МОСКВА, КВ.1;1225;100,00;100,00;0,00;5\r\n" +
	"=1;100,00;100,00;0,00;4242;06-01-2026\r\n"

type fakeRegistries struct {
	PaymentRegistryStore
	imported []string
}

func (f *fakeRegistries) BankAccountsByNumber(context.Context) (map[string]int64, error) {
	return map[string]int64{acct: 1, otherAc: 2}, nil
}

func (f *fakeRegistries) BankAccountsByRegistryCode(context.Context) (map[string]int64, error) {
	return map[string]int64{"vtb_1_002": 2}, nil
}

func (f *fakeRegistries) Relink(context.Context) (int, []store.UnmatchedRegistry, error) {
	return 2, []store.UnmatchedRegistry{{RegistryID: 5, FileName: "x.txt", Reason: store.UnmatchedNoPayment}}, nil
}

func (f *fakeRegistries) Import(_ context.Context, bankID int64, name string, _ []byte, _ *model.ParsedRegistry) (model.RegistryImportResult, error) {
	switch name {
	case "UPPER_" + acct + "_2.TXT":
		return model.RegistryImportResult{}, &store.RegistryExistsError{RegistryID: 7, FileName: "stored-earlier.txt"}
	case "dup2_" + acct + "_5.txt":
		return model.RegistryImportResult{}, &store.RegistryExistsError{RegistryID: 11, FileName: "short-name.txt"}
	case "dup_" + acct + "_3.txt":
		return model.RegistryImportResult{}, &store.AllDuplicatesError{Total: 1, Skipped: []model.SkippedPayment{{ExternalID: "900000000001"}}}
	}
	f.imported = append(f.imported, name)
	return model.RegistryImportResult{RegistryID: 10 + bankID, Created: 1, Linked: 1, SkippedDuplicates: 0}, nil
}

func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type upload struct{ name, body string }

func postRegistries(t *testing.T, fake *fakeRegistries, files ...upload) (int, map[string]any) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	h := NewRouter(Deps{Auth: testAuth(string(hash)), PaymentRegistries: fake})
	cookie := login(t, h)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, f := range files {
		part, err := mw.CreateFormFile("file", f.name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(f.body))
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payment-registries/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func statuses(out map[string]any) map[string]string {
	got := map[string]string{}
	for _, f := range out["files"].([]any) {
		m := f.(map[string]any)
		got[m["file_name"].(string)] = m["status"].(string)
	}
	return got
}

// ignoredOf: имя пропущенного файла → причина из ответа.
func ignoredOf(out map[string]any) map[string]string {
	got := map[string]string{}
	for _, f := range out["ignored"].([]any) {
		m := f.(map[string]any)
		got[m["file_name"].(string)] = m["reason"].(string)
	}
	return got
}

func TestImportReportsIgnoredPlainFilesAndCapsTheList(t *testing.T) {
	uploads := []upload{{"040126_VTB_231068_001.txt", registryText}, {"photo.jpg", "x"}}
	for i := 0; i < maxListedIgnored+5; i++ {
		uploads = append(uploads, upload{fmt.Sprintf("note%03d.txt", i), "hello"})
	}
	code, out := postRegistries(t, &fakeRegistries{}, uploads...)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if got := out["summary"].(map[string]any)["files_ignored"]; got != float64(maxListedIgnored+7) {
		t.Errorf("files_ignored = %v", got)
	}
	list := out["ignored"].([]any)
	if len(list) != maxListedIgnored {
		t.Errorf("listed = %d, want %d", len(list), maxListedIgnored)
	}
	// после сортировки по имени первым идёт файл ВТБ: его причину пользователь и должен увидеть
	if first := list[0].(map[string]any); first["file_name"] != "040126_VTB_231068_001.txt" || first["reason"] != "no_account" {
		t.Errorf("first = %v", first)
	}
	if code, out := postRegistries(t, &fakeRegistries{}, upload{"a_" + acct + "_1.txt", registryText}); code != http.StatusOK || len(out["ignored"].([]any)) != 0 {
		t.Errorf("nothing ignored: ignored = %v", out["ignored"])
	}
}

func TestImportZipFindsRegistriesByAccountInName(t *testing.T) {
	archive := zipOf(t, map[string]string{
		"2026/январь/900005_9715357654_" + acct + "_640.txt":            registryText,
		"__MACOSX/2026/январь/._900005_9715357654_" + acct + "_640.txt": "junk",
		"2026/январь/._900005_9715357654_" + acct + "_641.txt":          "junk",
		"2026/readme.txt":                   "not a registry",
		"2026/x_11111111111111111111_1.txt": registryText,
		"2026/UPPER_" + acct + "_2.TXT":     registryText,
		"dup_" + acct + "_3.txt":            registryText,
		"2026/ящик/dup2_" + acct + "_5.txt": registryText,
		"2026/" + otherAc + "_9.pdf":        "not txt",
		"2026/n/z.zip":                      "PK nested archives are not unpacked",
	})
	fake := &fakeRegistries{}
	code, out := postRegistries(t, fake, upload{"registries.zip", string(archive)})
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, out)
	}
	got := statuses(out)
	want := map[string]string{
		"registries.zip/2026/январь/900005_9715357654_" + acct + "_640.txt": "imported",
		"registries.zip/2026/x_11111111111111111111_1.txt":                  "unknown_account",
		"registries.zip/2026/UPPER_" + acct + "_2.TXT":                      "duplicate_file",
		"registries.zip/dup_" + acct + "_3.txt":                             "all_duplicates",
		"registries.zip/2026/ящик/dup2_" + acct + "_5.txt":                  "duplicate_file",
	}
	if len(got) != len(want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	for name, st := range want {
		if got[name] != st {
			t.Errorf("%s: status = %q, want %q", name, got[name], st)
		}
	}
	// в Import уходит имя файла без каталогов, счёт определён по номеру в имени
	if len(fake.imported) != 1 || fake.imported[0] != "900005_9715357654_"+acct+"_640.txt" {
		t.Errorf("imported = %v", fake.imported)
	}
	// имя файла-оригинала: для копии из этого же запроса полный путь, для ранее загруженного имя из базы
	dupOf := map[string]string{}
	for _, f := range out["files"].([]any) {
		m := f.(map[string]any)
		if d, ok := m["duplicate_of"].(string); ok {
			dupOf[m["file_name"].(string)] = d
		}
	}
	if dupOf["registries.zip/2026/UPPER_"+acct+"_2.TXT"] != "stored-earlier.txt" ||
		dupOf["registries.zip/2026/ящик/dup2_"+acct+"_5.txt"] != "registries.zip/2026/январь/900005_9715357654_"+acct+"_640.txt" {
		t.Errorf("duplicate_of = %v", dupOf)
	}
	// пропущенные называются поимённо, с причиной; служебные записи macOS не считаются
	if got := ignoredOf(out); len(got) != 3 || got["registries.zip/2026/readme.txt"] != "no_account" ||
		got["registries.zip/2026/"+otherAc+"_9.pdf"] != "not_txt" || got["registries.zip/2026/n/z.zip"] != "not_txt" {
		t.Errorf("ignored = %v", got)
	}
	s := out["summary"].(map[string]any)
	if s["files_imported"] != 1.0 || s["files_failed"] != 4.0 || s["files_ignored"] != 3.0 || s["payments_created"] != 1.0 || s["payments_skipped"] != 1.0 {
		t.Errorf("summary = %v", s)
	}
}

func TestImportPlainFilesAreIndependent(t *testing.T) {
	fake := &fakeRegistries{}
	code, out := postRegistries(t, fake,
		upload{"a_" + acct + "_1.txt", registryText},
		upload{"b_" + otherAc + "_2.txt", registryText},
		upload{"c_" + acct + "_3.txt", "garbage\r\n=1;1,00;1,00;0,00;1;01-01-2026\r\n"},
		upload{"notes.txt", "hello"},
	)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	got := statuses(out)
	if got["a_"+acct+"_1.txt"] != "imported" || got["b_"+otherAc+"_2.txt"] != "imported" || got["c_"+acct+"_3.txt"] != "invalid" || len(got) != 3 {
		t.Errorf("statuses = %v", got)
	}
	if len(fake.imported) != 2 {
		t.Errorf("one broken file must not stop the others: imported = %v", fake.imported)
	}
}

func TestImportRequiresFilesAndRejectsBadArchive(t *testing.T) {
	if code, _ := postRegistries(t, &fakeRegistries{}); code != http.StatusBadRequest {
		t.Errorf("no files: status = %d, want 400", code)
	}
	code, out := postRegistries(t, &fakeRegistries{}, upload{"broken.zip", "PK\x03\x04 this is not a zip"})
	if code != http.StatusOK || statuses(out)["broken.zip"] != "error" {
		t.Errorf("broken archive: status = %d, out = %v", code, out)
	}
}

func TestBaseNameAndTxt(t *testing.T) {
	if baseName(`dir\sub\file_`+acct+`_1.txt`) != "file_"+acct+"_1.txt" || !isTxt("A.TXT") || isTxt("a.txt.pdf") {
		t.Error("baseName/isTxt")
	}
}

func TestExpandZipLimits(t *testing.T) {
	var total int64
	files, err := expandZip("big.zip", zipOf(t, map[string]string{"x_" + acct + "_1.txt": string(make([]byte, maxRegistrySize+1))}), &total, registrySkipReason, maxRegistrySize, &ignoredFiles{})
	if err != nil || len(files) != 1 {
		t.Fatalf("files = %v, err = %v", files, err)
	}
	if _, err := files[0].open(); err == nil {
		t.Error("a file over the size limit must not be read")
	}

	old := maxArchiveEntries
	maxArchiveEntries = 3
	defer func() { maxArchiveEntries = old }()
	many := map[string]string{"a.txt": "", "b.txt": "", "c.txt": "", "d.txt": ""}
	if _, err := expandZip("many.zip", zipOf(t, many), &total, registrySkipReason, maxRegistrySize, &ignoredFiles{}); err == nil {
		t.Error("an archive with too many entries must be rejected")
	}
}

func TestGzippedRegistryUpload(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(registryText))
	_ = zw.Close()
	fake := &fakeRegistries{}
	code, out := postRegistries(t, fake, upload{"a_" + acct + "_1.txt.gz", buf.String()})
	if code != http.StatusOK || statuses(out)["a_"+acct+"_1.txt"] != "imported" || len(fake.imported) != 1 || fake.imported[0] != "a_"+acct+"_1.txt" {
		t.Errorf("gzipped registry: status = %d, out = %v, imported = %v", code, out, fake.imported)
	}
}

const vtbText = "03-01-2026;09-32-33;4;900000000001;a77c0963dd3d4706b36203e62;0000001001;Иванов Иван;г Москва, кв.1;1225;100.00;100.00;0.00\r\n" +
	"=1;100.00;100.00;0.00;4242;06-01-2026\r\n"

// У ВТБ в имени файла нет номера счёта: счёт определяется по коду реестра, дата в начале имени не учитывается.
func TestImportFindsAccountByRegistryCode(t *testing.T) {
	fake := &fakeRegistries{}
	code, out := postRegistries(t, fake,
		upload{"040126_VTB_1_002.txt", vtbText},
		upload{"050126_vtb_1_002.TXT", vtbText},
		upload{"040126_VTB_1_003.txt", vtbText},
	)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	got := statuses(out)
	if got["040126_VTB_1_002.txt"] != "imported" || got["050126_vtb_1_002.TXT"] != "imported" || len(got) != 2 {
		t.Errorf("statuses = %v", got)
	}
	if ig := ignoredOf(out); len(ig) != 1 || ig["040126_VTB_1_003.txt"] != "no_account" {
		t.Errorf("ignored = %v", ig)
	}
}

func TestRelinkEndpoint(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	h := NewRouter(Deps{Auth: testAuth(string(hash)), PaymentRegistries: &fakeRegistries{}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payment-registries/relink", nil)
	req.AddCookie(login(t, h))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out struct {
		Linked    int `json:"linked"`
		Unmatched []struct {
			RegistryID int64  `json:"registry_id"`
			Reason     string `json:"reason"`
		} `json:"unmatched"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK || out.Linked != 2 || len(out.Unmatched) != 1 || out.Unmatched[0].Reason != "no_payment" {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

type fakeCounterparties struct{ filter model.CounterpartyFilter }

func (f *fakeCounterparties) List(_ context.Context, flt model.CounterpartyFilter, _, _ int) ([]model.Counterparty, error) {
	f.filter = flt
	return []model.Counterparty{{Name: "ООО Тест", OutgoingCount: 1}}, nil
}

func TestCounterpartiesEndpoint(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	fake := &fakeCounterparties{}
	h := NewRouter(Deps{Auth: testAuth(string(hash)), Counterparties: fake})
	cookie := login(t, h)
	get := func(url string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := get("/api/v1/counterparties?direction=outgoing&q=%D0%BB%D0%B8%D1%84%D1%82"); rec.Code != http.StatusOK || fake.filter.Direction != "outgoing" || fake.filter.Q == nil || *fake.filter.Q != "лифт" {
		t.Errorf("status = %d, filter = %+v", rec.Code, fake.filter)
	}
	if rec := get("/api/v1/counterparties?direction=sideways"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad direction: status = %d", rec.Code)
	}
}
