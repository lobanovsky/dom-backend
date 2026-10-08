package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"dom-backend/internal/clientbank"
	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

type BankStatementStore interface {
	List(ctx context.Context, f model.BankStatementFilter, limit, offset int) ([]model.BankStatement, error)
	Get(ctx context.Context, id int64) (model.BankStatement, error)
	File(ctx context.Context, id int64) (string, []byte, error)
	Import(ctx context.Context, fileName string, data []byte, st *model.ParsedStatement) (model.StatementImportResult, error)
}

type bankStatementHandlers struct{ s BankStatementStore }

func (h bankStatementHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/bank-statements", h.list)
	mux.HandleFunc("POST /api/v1/bank-statements/import", h.importFiles)
	mux.HandleFunc("GET /api/v1/bank-statements/{id}", h.get)
	mux.HandleFunc("GET /api/v1/bank-statements/{id}/file", h.file)
}

func (h bankStatementHandlers) list(w http.ResponseWriter, r *http.Request) {
	p, ok := parseListSpec(w, r, listSpec{Ints: []string{"bank_account_id"}, Texts: []string{"q"}, Dates: []string{"date_from", "date_to"}})
	if !ok {
		return
	}
	items, err := h.s.List(r.Context(), model.BankStatementFilter{
		Deleted: p.Deleted, BankAccountID: p.Int("bank_account_id"), DateFrom: p.Date("date_from"), DateTo: p.Date("date_to"), Q: p.Text("q"),
	}, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h bankStatementHandlers) get(w http.ResponseWriter, r *http.Request) {
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

func (h bankStatementHandlers) file(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	name, data, err := h.s.File(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// Статусы файла выписки в отчёте: те же, что у реестров (fileImported и др.), с другим содержимым result.
type statementFileResult struct {
	FileName    string                       `json:"file_name"`
	Status      string                       `json:"status"`
	StatementID int64                        `json:"statement_id,omitempty"`
	DuplicateOf string                       `json:"duplicate_of,omitempty"`
	Result      *model.StatementImportResult `json:"result,omitempty"`
	Skipped     []model.SkippedOperation     `json:"skipped,omitempty"`
	Error       string                       `json:"error,omitempty"`
	Rows        []model.ImportRowError       `json:"rows,omitempty"`
}

type statementImportSummary struct {
	FilesImported     int `json:"files_imported"`
	FilesFailed       int `json:"files_failed"`
	FilesIgnored      int `json:"files_ignored"`
	IncomingCreated   int `json:"incoming_created"`
	OutgoingCreated   int `json:"outgoing_created"`
	OperationsSkipped int `json:"operations_skipped"`
}

// statementName: выписка — текстовый файл обмена с 1С (.txt); по содержимому это проверяется при разборе.
func statementName(base string) bool {
	return strings.EqualFold(baseExt(base), ".txt")
}

func baseExt(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i:]
	}
	return ""
}

// importFiles принимает multipart/form-data с полями file: файлы обмена 1С (формат 1CClientBankExchange 1.03, .txt) и zip-архивы с ними.
// Наши счета берутся из файла и должны быть заведены в системе; файл с несколькими счетами даёт выписку на каждый. Каждый файл грузится отдельной транзакцией; ответ 200: {files, summary}.
func (h bankStatementHandlers) importFiles(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		// Обрыв загрузки (таймаут прокси, закрытая вкладка) виден здесь как unexpected EOF.
		slog.Warn("upload failed", "path", r.URL.Path, "content_length", r.ContentLength, "err", err)
		writeError(w, http.StatusBadRequest, "invalid multipart body: "+err.Error())
		return
	}
	defer r.MultipartForm.RemoveAll()
	headers := r.MultipartForm.File["file"]
	if len(headers) == 0 {
		writeError(w, http.StatusBadRequest, `field "file" with statement files or a zip archive is required`)
		return
	}

	var (
		candidates []registryFile
		unpacked   int64
		results    []statementFileResult
		summary    statementImportSummary
	)
	for _, hd := range headers {
		data, err := readUpload(hd.Open, uploadLimit(hd.Filename, maxStatementSize))
		switch {
		case err != nil:
			results = append(results, statementFileResult{FileName: validUTF8(hd.Filename), Status: fileError, Error: err.Error()})
		case isZip(hd.Filename, data):
			files, skipped, err := expandZip(hd.Filename, data, &unpacked, statementName, maxStatementSize)
			summary.FilesIgnored += skipped
			if err != nil {
				results = append(results, statementFileResult{FileName: validUTF8(hd.Filename), Status: fileError, Error: err.Error()})
				continue
			}
			candidates = append(candidates, files...)
		default:
			name, data, err := inflate(hd.Filename, data, maxStatementSize)
			if err != nil {
				results = append(results, statementFileResult{FileName: validUTF8(hd.Filename), Status: fileError, Error: err.Error()})
				continue
			}
			candidates = append(candidates, plainFile(name, data))
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })

	loadedAs := map[int64]string{}
	for _, f := range candidates {
		base := baseName(f.Name)
		if !statementName(base) {
			summary.FilesIgnored++
			continue
		}
		if data, err := f.open(); err == nil && !clientbank.LooksLike(data) {
			summary.FilesIgnored++ // .txt, но не файл обмена с 1С
			continue
		}
		for _, fr := range h.importOne(r.Context(), f, base) {
			switch fr.Status {
			case fileImported:
				loadedAs[fr.Result.StatementID] = fr.FileName
			case fileDuplicate:
				if path, ok := loadedAs[fr.StatementID]; ok {
					fr.DuplicateOf = path
				}
			}
			results = append(results, fr)
		}
	}

	for _, fr := range results {
		logStatementFile(fr)
		switch fr.Status {
		case fileImported:
			summary.FilesImported++
			summary.IncomingCreated += fr.Result.Incoming
			summary.OutgoingCreated += fr.Result.Outgoing
			summary.OperationsSkipped += fr.Result.SkippedDuplicates
		default:
			summary.FilesFailed++
			summary.OperationsSkipped += len(fr.Skipped)
		}
	}
	if results == nil {
		results = []statementFileResult{}
	}
	slog.Info("bank statements import", "files", len(results), "imported", summary.FilesImported, "failed", summary.FilesFailed,
		"ignored", summary.FilesIgnored, "incoming", summary.IncomingCreated, "outgoing", summary.OutgoingCreated, "skipped", summary.OperationsSkipped)
	writeJSON(w, http.StatusOK, map[string]any{"files": results, "summary": summary})
}

// importOne разбирает файл и загружает выписку по каждому нашему счёту (в файле их может быть несколько).
// Результатов столько же, сколько выписок; если разобрать не удалось — один результат с причиной.
func (h bankStatementHandlers) importOne(ctx context.Context, f registryFile, base string) []statementFileResult {
	data, err := f.open()
	if err != nil {
		return []statementFileResult{{FileName: f.Name, Status: fileError, Error: err.Error()}}
	}
	statements, rowErrs, err := clientbank.Parse(data)
	switch {
	case err != nil:
		return []statementFileResult{{FileName: f.Name, Status: fileInvalid, Error: err.Error()}}
	case len(rowErrs) > 0:
		return []statementFileResult{{FileName: f.Name, Status: fileInvalid, Error: "file contains invalid rows, nothing was imported", Rows: rowErrs}}
	}
	var out []statementFileResult
	for _, st := range statements {
		name, shown := base, f.Name
		if st.MultiPart {
			name, shown = base+" — счёт "+st.Part, f.Name+" — счёт "+st.Part
		}
		out = append(out, h.importStatement(ctx, name, shown, data, st))
	}
	return out
}

func (h bankStatementHandlers) importStatement(ctx context.Context, name, shown string, data []byte, st *model.ParsedStatement) statementFileResult {
	fr := statementFileResult{FileName: shown}
	res, err := h.s.Import(ctx, name, data, st)
	var exists *store.StatementExistsError
	var allDup *store.StatementAllDuplicatesError
	var unknown *store.UnknownBankAccountError
	var se *store.Error
	switch {
	case err == nil:
		fr.Status, fr.Result = fileImported, &res
	case errors.As(err, &exists):
		fr.Status, fr.StatementID, fr.DuplicateOf = fileDuplicate, exists.StatementID, exists.FileName
	case errors.As(err, &allDup):
		fr.Status, fr.Skipped = fileAllDuplicates, allDup.Skipped
	case errors.As(err, &unknown):
		fr.Status, fr.Error = fileUnknownAccount, unknown.Error()
	case errors.As(err, &se):
		fr.Status, fr.Error = fileError, se.Msg
	default:
		slog.Error("statement import", "file", shown, "err", err)
		fr.Status, fr.Error = fileError, "internal error"
	}
	return fr
}

func contains(items []string, v string) bool {
	for _, s := range items {
		if s == v {
			return true
		}
	}
	return false
}

// logStatementFile пишет итог по файлу: причина отказа попадает в журнал, чтобы её можно было найти без браузера.
func logStatementFile(fr statementFileResult) {
	attrs := []any{"file", fr.FileName, "status", fr.Status}
	if fr.Result != nil {
		attrs = append(attrs, "statement_id", fr.Result.StatementID, "incoming", fr.Result.Incoming, "outgoing", fr.Result.Outgoing, "skipped", fr.Result.SkippedDuplicates)
	}
	if fr.Error != "" {
		attrs = append(attrs, "error", fr.Error)
	}
	if len(fr.Rows) > 0 {
		attrs = append(attrs, "bad_rows", len(fr.Rows), "first_bad_row", fr.Rows[0])
	}
	if fr.Status == fileImported || fr.Status == fileDuplicate || fr.Status == fileAllDuplicates {
		slog.Info("statement file", attrs...)
		return
	}
	slog.Warn("statement file", attrs...)
}
