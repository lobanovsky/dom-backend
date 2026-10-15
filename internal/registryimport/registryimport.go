// Package registryimport загружает файлы реестров платежей (Сбер, ВТБ) без привязки к HTTP:
// им пользуются и ручная загрузка (httpapi), и забор реестров из почты (mailsync).
package registryimport

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"

	"dom-backend/internal/model"
	"dom-backend/internal/sberregistry"
	"dom-backend/internal/store"
)

// Статусы файла в отчёте о загрузке (коды; русские подписи — во фронтенде).
const (
	Imported       = "imported"
	Duplicate      = "duplicate_file"  // тот же файл уже загружен
	AllDuplicates  = "all_duplicates"  // все платежи файла уже есть в базе
	UnknownAccount = "unknown_account" // в имени есть номер счёта, которого нет в системе
	Invalid        = "invalid"         // файл не разобран: нарушен формат или итоги
	Error          = "error"           // другая ошибка (например, файл слишком большой)
)

// Причины, по которым файл не принят за реестр.
const (
	IgnoreNotTxt    = "not_txt"    // расширение не .txt
	IgnoreNoAccount = "no_account" // в имени нет номера счёта и кода реестра, известного системе
)

// Пределы для вложений из почты (как у ручной загрузки): файлы читаются в память.
const (
	MaxFileSize    = 5 << 20  // один файл реестра
	MaxArchiveSize = 50 << 20 // zip-архив
	maxUnpacked    = 50 << 20 // всё распакованное из одного архива
)

type FileResult struct {
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

// File — кандидат в реестр: имя (для архива — путь внутри него) и ленивое чтение содержимого.
type File struct {
	Name string
	Open func() ([]byte, error)
}

// Plain — обычный файл с уже прочитанным содержимым.
func Plain(name string, data []byte) File {
	return File{Name: ValidUTF8(name), Open: func() ([]byte, error) { return data, nil }}
}

type Store interface {
	Import(ctx context.Context, bankAccountID int64, fileName string, data []byte, reg *model.ParsedRegistry) (model.RegistryImportResult, error)
	BankAccountsByNumber(ctx context.Context) (map[string]int64, error)
	BankAccountsByRegistryCode(ctx context.Context) (map[string]int64, error)
}

type Importer struct{ Store Store }

// Accounts — счета системы, по которым определяется, к какому счёту относится файл.
type Accounts struct {
	byNumber map[string]int64
	byCode   map[string]int64
}

func (im Importer) Accounts(ctx context.Context) (Accounts, error) {
	n, err := im.Store.BankAccountsByNumber(ctx)
	if err != nil {
		return Accounts{}, err
	}
	c, err := im.Store.BankAccountsByRegistryCode(ctx)
	if err != nil {
		return Accounts{}, err
	}
	return Accounts{byNumber: n, byCode: c}, nil
}

// BaseName — имя файла без каталогов; пути из архива могут быть с обратными слэшами.
func BaseName(name string) string { return path.Base(strings.ReplaceAll(name, "\\", "/")) }

func IsTxt(name string) bool { return strings.EqualFold(path.Ext(name), ".txt") }

func ValidUTF8(s string) string { return strings.ToValidUTF8(s, "?") }

// Import загружает один файл. Если файл не реестр какого-либо счёта системы (не .txt или в имени нет ни номера счёта,
// ни известного кода реестра), результат пуст, а ignore содержит причину (IgnoreNotTxt, IgnoreNoAccount).
// Счёт определяется по 20-значному числу в имени, совпавшему с номером банковского счёта, а если числа нет (ВТБ) —
// по коду реестра в имени (bank_accounts.registry_codes). Номер счёта, которого нет в системе, даёт UnknownAccount.
func (im Importer) Import(ctx context.Context, acc Accounts, f File) (res FileResult, ignore string) {
	base := BaseName(f.Name)
	if !IsTxt(base) {
		return FileResult{}, IgnoreNotTxt
	}
	numbers := sberregistry.AccountsInName(base)
	bankID := int64(0)
	for _, a := range numbers {
		if id, ok := acc.byNumber[a]; ok {
			bankID = id
			break
		}
	}
	if bankID == 0 {
		bankID = acc.byCode[sberregistry.FileCode(base)]
	}
	if bankID == 0 && len(numbers) == 0 {
		return FileResult{}, IgnoreNoAccount
	}
	if bankID == 0 {
		return FileResult{FileName: f.Name, Status: UnknownAccount, Error: "bank account " + numbers[0] + " is not in the system"}, ""
	}
	return im.importOne(ctx, f, base, bankID), ""
}

func (im Importer) importOne(ctx context.Context, f File, base string, bankID int64) FileResult {
	fr := FileResult{FileName: f.Name, BankAccountID: bankID}
	data, err := f.Open()
	if err != nil {
		fr.Status, fr.Error = Error, err.Error()
		return fr
	}
	reg, rowErrs, err := sberregistry.Parse(base, data)
	switch {
	case err != nil:
		fr.Status, fr.Error = Invalid, err.Error()
		return fr
	case len(rowErrs) > 0:
		fr.Status, fr.Error, fr.Rows = Invalid, "file contains invalid rows, nothing was imported", rowErrs
		return fr
	}
	res, err := im.Store.Import(ctx, bankID, base, data, reg)
	var exists *store.RegistryExistsError
	var allDup *store.AllDuplicatesError
	var se *store.Error
	switch {
	case err == nil:
		fr.Status, fr.Result = Imported, &res
	case errors.As(err, &exists):
		fr.Status, fr.RegistryID, fr.DuplicateOf = Duplicate, exists.RegistryID, exists.FileName
	case errors.As(err, &allDup):
		fr.Status, fr.Skipped = AllDuplicates, allDup.Skipped
	case errors.As(err, &se):
		fr.Status, fr.Error = Error, se.Msg
	default:
		slog.Error("registry import", "file", f.Name, "err", err)
		fr.Status, fr.Error = Error, "internal error"
	}
	return fr
}

// Expand раскрывает вложение письма в кандидаты в реестры: .txt берётся как есть, .txt.gz распаковывается,
// из .zip берутся все .txt (служебные записи macOS и каталоги пропускаются, вложенные архивы не раскрываются).
// Остальные вложения не реестры: возвращается пустой список без ошибки.
func Expand(name string, data []byte) ([]File, error) {
	switch strings.ToLower(path.Ext(name)) {
	case ".zip":
		return expandZip(name, data)
	case ".gz":
		if !IsTxt(strings.TrimSuffix(name, path.Ext(name))) {
			return nil, nil
		}
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("not a valid gzip file")
		}
		defer zr.Close()
		out, err := io.ReadAll(io.LimitReader(zr, MaxFileSize+1))
		if err != nil {
			return nil, fmt.Errorf("not a valid gzip file")
		}
		if len(out) > MaxFileSize {
			return nil, fmt.Errorf("file is too large when unpacked (max %d MB)", MaxFileSize>>20)
		}
		return []File{Plain(strings.TrimSuffix(name, path.Ext(name)), out)}, nil
	case ".txt":
		if len(data) > MaxFileSize {
			return nil, fmt.Errorf("file is too large (max %d MB)", MaxFileSize>>20)
		}
		return []File{Plain(name, data)}, nil
	}
	return nil, nil
}

func expandZip(archive string, data []byte) ([]File, error) {
	if len(data) > MaxArchiveSize {
		return nil, fmt.Errorf("archive is too large (max %d MB)", MaxArchiveSize>>20)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a valid zip archive")
	}
	var unpacked int64
	var files []File
	for _, zf := range zr.File {
		name := strings.ReplaceAll(zf.Name, "\\", "/")
		if zf.FileInfo().IsDir() || strings.HasPrefix(name, "__MACOSX/") || strings.Contains(name, "/__MACOSX/") ||
			strings.HasPrefix(path.Base(name), "._") || !IsTxt(path.Base(name)) {
			continue
		}
		zf := zf
		files = append(files, File{
			Name: ValidUTF8(archive + "/" + name),
			Open: func() ([]byte, error) {
				if zf.UncompressedSize64 > MaxFileSize {
					return nil, fmt.Errorf("file is too large (max %d MB)", MaxFileSize>>20)
				}
				rc, err := zf.Open()
				if err != nil {
					return nil, fmt.Errorf("cannot read the archive entry")
				}
				defer rc.Close()
				// Заголовок архива может лгать о размере: читаем с ограничением по факту.
				b, err := io.ReadAll(io.LimitReader(rc, MaxFileSize+1))
				if err != nil || len(b) > MaxFileSize {
					return nil, fmt.Errorf("cannot read the archive entry (max %d MB)", MaxFileSize>>20)
				}
				unpacked += int64(len(b))
				if unpacked > maxUnpacked {
					return nil, fmt.Errorf("archive is too large when unpacked (max %d MB)", maxUnpacked>>20)
				}
				return b, nil
			},
		})
	}
	return files, nil
}
