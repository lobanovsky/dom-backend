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
	"dom-backend/internal/sberregistry"
	"dom-backend/internal/store"
)

type PaymentRegistryStore interface {
	List(ctx context.Context, f model.PaymentRegistryFilter, limit, offset int) ([]model.PaymentRegistry, error)
	Get(ctx context.Context, id int64) (model.PaymentRegistry, error)
	File(ctx context.Context, id int64) (string, []byte, error)
	Import(ctx context.Context, bankAccountID int64, fileName string, data []byte, reg *model.ParsedRegistry) (model.RegistryImportResult, error)
	BankAccountsByNumber(ctx context.Context) (map[string]int64, error)
}

type paymentRegistryHandlers struct{ s PaymentRegistryStore }

func (h paymentRegistryHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/payment-registries", h.list)
	mux.HandleFunc("POST /api/v1/payment-registries/import", h.importFiles)
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

// Статусы файла в отчёте о загрузке.
const (
	fileImported       = "imported"
	fileDuplicate      = "duplicate_file"  // тот же файл уже загружен
	fileAllDuplicates  = "all_duplicates"  // все платежи файла уже есть в базе
	fileUnknownAccount = "unknown_account" // в имени есть номер счёта, которого нет в системе
	fileInvalid        = "invalid"         // файл не разобран: нарушен формат или итоги
	fileError          = "error"           // другая ошибка (например, файл слишком большой)
)

type registryFileResult struct {
	FileName      string                      `json:"file_name"`
	Status        string                      `json:"status"`
	BankAccountID int64                       `json:"bank_account_id,omitempty"`
	RegistryID    int64                       `json:"registry_id,omitempty"`
	DuplicateOf   string                      `json:"duplicate_of,omitempty"` // для duplicate_file: имя файла, с которым совпало содержимое
	Result        *model.RegistryImportResult `json:"result,omitempty"`
	Skipped       []model.SkippedPayment      `json:"skipped,omitempty"`
	Error         string                      `json:"error,omitempty"`
	Rows          []model.ImportRowError      `json:"rows,omitempty"`
}

type registryImportSummary struct {
	FilesImported   int `json:"files_imported"`
	FilesFailed     int `json:"files_failed"`
	FilesIgnored    int `json:"files_ignored"`
	PaymentsCreated int `json:"payments_created"`
	PaymentsSkipped int `json:"payments_skipped"`
	Linked          int `json:"linked"`
	Unlinked        int `json:"unlinked"`
}

// importFiles принимает multipart/form-data с полями file: реестры (.txt) и zip-архивы с реестрами.
//
// Реестр — файл .txt, в имени которого есть 20-значное число, совпадающее с номером банковского счёта
// в системе; счёт определяется по нему. Остальные файлы игнорируются (считаются в summary), а файл с номером
// счёта, которого нет в системе, попадает в отчёт со статусом unknown_account. Каждый файл загружается
// отдельной транзакцией: ошибка в одном не отменяет остальные. Ответ 200: {files, summary}.
func (h paymentRegistryHandlers) importFiles(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
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
	)
	for _, hd := range headers {
		data, err := readUpload(hd.Open, uploadLimit(hd.Filename, maxRegistrySize))
		switch {
		case err != nil:
			results = append(results, registryFileResult{FileName: validUTF8(hd.Filename), Status: fileError, Error: err.Error()})
		case isZip(hd.Filename, data):
			files, skipped, err := expandZip(hd.Filename, data, &unpacked, registryName, maxRegistrySize)
			summary.FilesIgnored += skipped
			if err != nil {
				results = append(results, registryFileResult{FileName: validUTF8(hd.Filename), Status: fileError, Error: err.Error()})
				continue
			}
			candidates = append(candidates, files...)
		default:
			candidates = append(candidates, plainFile(hd.Filename, data))
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })

	known, err := h.s.BankAccountsByNumber(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	loadedAs := map[int64]string{} // id реестра, созданного в этом запросе → полный путь файла (в том числе внутри архива)
	for _, f := range candidates {
		base := baseName(f.Name)
		accounts := sberregistry.AccountsInName(base)
		if !isTxt(base) || len(accounts) == 0 {
			summary.FilesIgnored++
			continue
		}
		bankID := int64(0)
		for _, a := range accounts {
			if id, ok := known[a]; ok {
				bankID = id
				break
			}
		}
		if bankID == 0 {
			results = append(results, registryFileResult{FileName: f.Name, Status: fileUnknownAccount, Error: "bank account " + accounts[0] + " is not in the system"})
			continue
		}
		fr := h.importOne(r.Context(), f, base, bankID)
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

	for _, fr := range results {
		logRegistryFile(fr)
		switch fr.Status {
		case fileImported:
			summary.FilesImported++
			summary.PaymentsCreated += fr.Result.Created
			summary.PaymentsSkipped += fr.Result.SkippedDuplicates
			summary.Linked += fr.Result.Linked
			summary.Unlinked += fr.Result.Unlinked
		default:
			summary.FilesFailed++
			summary.PaymentsSkipped += len(fr.Skipped)
		}
	}
	if results == nil {
		results = []registryFileResult{}
	}
	slog.Info("payment registries import", "files", len(results), "imported", summary.FilesImported, "failed", summary.FilesFailed,
		"ignored", summary.FilesIgnored, "payments", summary.PaymentsCreated, "skipped", summary.PaymentsSkipped, "linked", summary.Linked, "unlinked", summary.Unlinked)
	writeJSON(w, http.StatusOK, map[string]any{"files": results, "summary": summary})
}

func (h paymentRegistryHandlers) importOne(ctx context.Context, f registryFile, base string, bankID int64) registryFileResult {
	fr := registryFileResult{FileName: f.Name, BankAccountID: bankID}
	data, err := f.open()
	if err != nil {
		fr.Status, fr.Error = fileError, err.Error()
		return fr
	}
	reg, rowErrs, err := sberregistry.Parse(base, data)
	switch {
	case err != nil:
		fr.Status, fr.Error = fileInvalid, err.Error()
		return fr
	case len(rowErrs) > 0:
		fr.Status, fr.Error, fr.Rows = fileInvalid, "file contains invalid rows, nothing was imported", rowErrs
		return fr
	}
	res, err := h.s.Import(ctx, bankID, base, data, reg)
	var exists *store.RegistryExistsError
	var allDup *store.AllDuplicatesError
	var se *store.Error
	switch {
	case err == nil:
		fr.Status, fr.Result = fileImported, &res
	case errors.As(err, &exists):
		fr.Status, fr.RegistryID, fr.DuplicateOf = fileDuplicate, exists.RegistryID, exists.FileName
	case errors.As(err, &allDup):
		fr.Status, fr.Skipped = fileAllDuplicates, allDup.Skipped
	case errors.As(err, &se):
		fr.Status, fr.Error = fileError, se.Msg
	default:
		slog.Error("registry import", "file", f.Name, "err", err)
		fr.Status, fr.Error = fileError, "internal error"
	}
	return fr
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
