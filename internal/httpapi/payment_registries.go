package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"dom-backend/internal/model"
	"dom-backend/internal/registryimport"
	"dom-backend/internal/store"
)

type PaymentRegistryStore interface {
	List(ctx context.Context, f model.PaymentRegistryFilter, limit, offset int) ([]model.PaymentRegistry, error)
	Get(ctx context.Context, id int64) (model.PaymentRegistry, error)
	File(ctx context.Context, id int64) (string, []byte, error)
	Import(ctx context.Context, bankAccountID int64, fileName string, data []byte, reg *model.ParsedRegistry) (model.RegistryImportResult, error)
	BankAccountsByNumber(ctx context.Context) (map[string]int64, error)
	BankAccountsByRegistryCode(ctx context.Context) (map[string]int64, error)
	Relink(ctx context.Context) (int, []store.UnmatchedRegistry, error)
}

type paymentRegistryHandlers struct{ s PaymentRegistryStore }

func (h paymentRegistryHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/payment-registries", h.list)
	mux.HandleFunc("POST /api/v1/payment-registries/import", h.importFiles)
	mux.HandleFunc("POST /api/v1/payment-registries/relink", h.relink)
	mux.HandleFunc("GET /api/v1/payment-registries/{id}", h.get)
	mux.HandleFunc("GET /api/v1/payment-registries/{id}/file", h.file)
}

func (h paymentRegistryHandlers) list(w http.ResponseWriter, r *http.Request) {
	f, p, ok := paymentRegistryFilter(w, r)
	if !ok {
		return
	}
	items, err := h.s.List(r.Context(), f, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h paymentRegistryHandlers) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := h.s.Get(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// relink повторяет сопоставление реестров с платежами выписок: {linked, unmatched:[{registry_id, file_name, reason}]}.
func (h paymentRegistryHandlers) relink(w http.ResponseWriter, r *http.Request) {
	linked, unmatched, err := h.s.Relink(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"linked": linked, "unmatched": unmatched})
}

// file отдаёт исходный загруженный файл реестра.
func (h paymentRegistryHandlers) file(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	name, data, err := h.s.File(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=windows-1251")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// Статусы файла в отчёте о загрузке и результат по файлу — в registryimport (общие с загрузкой из почты).
const (
	fileImported       = registryimport.Imported
	fileDuplicate      = registryimport.Duplicate
	fileAllDuplicates  = registryimport.AllDuplicates
	fileUnknownAccount = registryimport.UnknownAccount
	fileInvalid        = registryimport.Invalid
	fileError          = registryimport.Error
)

type registryFileResult = registryimport.FileResult

type registryImportSummary struct {
	FilesImported int `json:"files_imported"`
	FilesFailed   int `json:"files_failed"`
	FilesIgnored  int `json:"files_ignored"`
	// Загруженные реестры, для которых платёж в выписке пока не найден (свяжутся, когда выписка придёт).
	FilesWithoutPayment int `json:"files_without_payment"`
	PaymentsCreated     int `json:"payments_created"`
	PaymentsSkipped     int `json:"payments_skipped"`
	Linked              int `json:"linked"`
	Unlinked            int `json:"unlinked"`
}

// importFiles принимает multipart/form-data с полями file: реестры (.txt) и zip-архивы с реестрами.
//
// Реестр — файл .txt. Счёт определяется по 20-значному числу в имени, совпавшему с номером банковского счёта
// в системе, а если числа нет (ВТБ) — по коду реестра в имени (bank_accounts.registry_codes). Остальные файлы
// игнорируются (в ignored с причиной), а файл с номером счёта, которого нет в системе, попадает в отчёт
// со статусом unknown_account. Каждый файл загружается
// отдельной транзакцией: ошибка в одном не отменяет остальные. Ответ 200: {files, summary}.
func (h paymentRegistryHandlers) importFiles(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		slog.Warn("upload failed", "path", r.URL.Path, "content_length", r.ContentLength, "err", err)
		writeError(w, http.StatusBadRequest, "invalid multipart body: "+err.Error())
		return
	}
	defer r.MultipartForm.RemoveAll()
	headers := r.MultipartForm.File["file"]
	if len(headers) == 0 {
		writeError(w, http.StatusBadRequest, `field "file" with registry files or a zip archive is required`)
		return
	}

	var (
		candidates []registryFile
		unpacked   int64
		results    []registryFileResult
		summary    registryImportSummary
		ignored    ignoredFiles
	)
	for _, hd := range headers {
		data, err := readUpload(hd.Open, uploadLimit(hd.Filename, maxRegistrySize))
		switch {
		case err != nil:
			results = append(results, registryFileResult{FileName: validUTF8(hd.Filename), Status: fileError, Error: err.Error()})
		case isZip(hd.Filename, data):
			files, err := expandZip(hd.Filename, data, &unpacked, registrySkipReason, maxRegistrySize, &ignored)
			if err != nil {
				results = append(results, registryFileResult{FileName: validUTF8(hd.Filename), Status: fileError, Error: err.Error()})
				continue
			}
			candidates = append(candidates, files...)
		default:
			name, data, err := inflate(hd.Filename, data, maxRegistrySize)
			if err != nil {
				results = append(results, registryFileResult{FileName: validUTF8(hd.Filename), Status: fileError, Error: err.Error()})
				continue
			}
			candidates = append(candidates, plainFile(name, data))
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })

	imp := registryimport.Importer{Store: h.s}
	accounts, err := imp.Accounts(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	loadedAs := map[int64]string{} // id реестра, созданного в этом запросе → полный путь файла (в том числе внутри архива)
	for _, f := range candidates {
		fr, ignore := imp.Import(r.Context(), accounts, registryimport.File{Name: f.Name, Open: f.open})
		if ignore != "" {
			ignored.add(f.Name, ignore)
			continue
		}
		switch fr.Status {
		case fileImported:
			loadedAs[fr.Result.RegistryID] = f.Name
		case fileDuplicate:
			// Копия файла из этого же запроса: полный путь понятнее имени без каталогов.
			if path, ok := loadedAs[fr.RegistryID]; ok {
				fr.DuplicateOf = path
			}
		}
		results = append(results, fr)
	}

	summary.FilesIgnored = ignored.total
	for _, fr := range results {
		logRegistryFile(fr)
		switch fr.Status {
		case fileImported:
			summary.FilesImported++
			summary.PaymentsCreated += fr.Result.Created
			summary.PaymentsSkipped += fr.Result.SkippedDuplicates
			summary.Linked += fr.Result.Linked
			summary.Unlinked += fr.Result.Unlinked
			if fr.Result.Payment == nil {
				summary.FilesWithoutPayment++
			}
		default:
			summary.FilesFailed++
			summary.PaymentsSkipped += len(fr.Skipped)
		}
	}
	if results == nil {
		results = []registryFileResult{}
	}
	slog.Info("payment registries import", "files", len(results), "imported", summary.FilesImported, "failed", summary.FilesFailed,
		"ignored", summary.FilesIgnored, "ignored_files", ignoredNames(ignored.listed, 5), "payments", summary.PaymentsCreated, "skipped", summary.PaymentsSkipped, "without_payment", summary.FilesWithoutPayment, "linked", summary.Linked, "unlinked", summary.Unlinked)
	if ignored.listed == nil {
		ignored.listed = []ignoredFile{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": results, "summary": summary, "ignored": ignored.listed})
}

// ignoredNames — первые n имён пропущенных файлов для журнала.
func ignoredNames(list []ignoredFile, n int) []string {
	names := make([]string, 0, n)
	for _, f := range list {
		if len(names) == n {
			break
		}
		names = append(names, f.FileName)
	}
	return names
}

// readUpload читает загруженный файл с ограничением размера limit (архив — maxArchiveSize).
func readUpload(open func() (multipart.File, error), limit int64) ([]byte, error) {
	f, err := open()
	if err != nil {
		return nil, errors.New("cannot read the uploaded file")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, errors.New("cannot read the uploaded file")
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file is too large (max %d MB)", limit>>20)
	}
	return data, nil
}

// uploadLimit: архив может быть больше одиночного файла.
func uploadLimit(filename string, single int64) int64 {
	if isZip(filename, nil) {
		return maxArchiveSize
	}
	return single
}

// logRegistryFile пишет итог по файлу реестра: причина отказа попадает в журнал.
func logRegistryFile(fr registryFileResult) {
	attrs := []any{"file", fr.FileName, "status", fr.Status}
	if fr.Result != nil {
		attrs = append(attrs, "registry_id", fr.Result.RegistryID, "created", fr.Result.Created, "linked", fr.Result.Linked, "unlinked", fr.Result.Unlinked, "skipped", fr.Result.SkippedDuplicates)
	}
	if fr.Error != "" {
		attrs = append(attrs, "error", fr.Error)
	}
	if len(fr.Rows) > 0 {
		attrs = append(attrs, "bad_rows", len(fr.Rows), "first_bad_row", fr.Rows[0])
	}
	if fr.Status == fileImported || fr.Status == fileDuplicate || fr.Status == fileAllDuplicates {
		slog.Info("registry file", attrs...)
		return
	}
	slog.Warn("registry file", attrs...)
}
