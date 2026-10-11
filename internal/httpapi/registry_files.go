package httpapi

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"strings"
)

// Пределы загрузки реестров. Файлы читаются в память, на диск ничего не пишется.
const (
	maxRegistrySize  = 5 << 20   // один файл реестра
	maxStatementSize = 50 << 20  // один файл выписки (1С за несколько лет — десятки мегабайт)
	maxArchiveSize   = 50 << 20  // сам zip-архив
	maxUnpackedTotal = 200 << 20 // всё распакованное за один запрос
	maxRequestSize   = 100 << 20
)

// maxArchiveEntries — предел числа записей в архиве. Он почти не защищает: размер архива и так ограничен
// (maxArchiveSize), а распаковываются только подходящие по имени файлы. Переменная, чтобы тест мог её уменьшить.
var maxArchiveEntries = 1_000_000

// Причины, по которым файл не принят за реестр или выписку (коды; русские подписи — во фронтенде).
const (
	ignoreNotTxt    = "not_txt"    // расширение не .txt
	ignoreNoAccount = "no_account" // в имени нет номера счёта
	ignoreNot1C     = "not_1c"     // .txt, но не файл обмена с 1С
)

// maxListedIgnored — сколько пропущенных файлов называется в ответе поимённо: архив на тысячи файлов не должен раздувать ответ.
const maxListedIgnored = 50

type ignoredFile struct {
	FileName string `json:"file_name"`
	Reason   string `json:"reason"`
}

// ignoredFiles считает пропущенные файлы и запоминает имена первых maxListedIgnored.
type ignoredFiles struct {
	total  int
	listed []ignoredFile
}

func (l *ignoredFiles) add(name, reason string) {
	l.total++
	if len(l.listed) < maxListedIgnored {
		l.listed = append(l.listed, ignoredFile{FileName: validUTF8(name), Reason: reason})
	}
}

// registryFile — кандидат в реестр: имя для отчёта (путь внутри архива) и ленивое чтение содержимого,
// чтобы не распаковывать файлы, которые заведомо не реестры.
type registryFile struct {
	Name string
	open func() ([]byte, error)
}

// baseName — имя файла без каталогов; пути из архива могут быть с обратными слэшами.
func baseName(name string) string {
	return path.Base(strings.ReplaceAll(name, "\\", "/"))
}

func isTxt(name string) bool { return strings.EqualFold(path.Ext(name), ".txt") }

// isZip: архив определяется по расширению .zip. По сигнатуре нельзя: файл .xlsx сам является zip-контейнером.
func isZip(name string, _ []byte) bool {
	return strings.EqualFold(path.Ext(name), ".zip")
}

func validUTF8(s string) string { return strings.ToValidUTF8(s, "?") }

// expandZip возвращает файлы архива, для которых skip(basename) вернул пустую строку (например, .txt с номером счёта в имени);
// остальные записываются в ign вместе с причиной и сразу отбрасываются, чтобы не держать в памяти список всех записей большого архива.
// Служебные записи macOS (__MACOSX/, ._имя) и каталоги пропускаются без счёта. Вложенные архивы не раскрываются.
// unpacked — общий счётчик распакованных байт на запрос.
func expandZip(archiveName string, data []byte, unpacked *int64, skip func(base string) string, maxEntry int64, ign *ignoredFiles) (files []registryFile, err error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a valid zip archive")
	}
	if len(zr.File) > maxArchiveEntries {
		return nil, fmt.Errorf("archive has too many entries (max %d)", maxArchiveEntries)
	}
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if f.FileInfo().IsDir() || strings.HasPrefix(name, "__MACOSX/") || strings.Contains(name, "/__MACOSX/") || strings.HasPrefix(path.Base(name), "._") {
			continue
		}
		if reason := skip(path.Base(name)); reason != "" {
			ign.add(archiveName+"/"+name, reason)
			continue
		}
		f := f
		files = append(files, registryFile{
			Name: validUTF8(archiveName + "/" + name),
			open: func() ([]byte, error) { return readZipEntry(f, unpacked, maxEntry) },
		})
	}
	return files, nil
}

// readZipEntry читает запись с ограничением по фактическому объёму: заголовок архива может лгать о размере.
func readZipEntry(f *zip.File, unpacked *int64, maxEntry int64) ([]byte, error) {
	if int64(f.UncompressedSize64) > maxEntry {
		return nil, fmt.Errorf("file is too large (max %d MB)", maxEntry>>20)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("cannot read the archive entry")
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxEntry+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read the archive entry")
	}
	if int64(len(data)) > maxEntry {
		return nil, fmt.Errorf("file is too large (max %d MB)", maxEntry>>20)
	}
	*unpacked += int64(len(data))
	if *unpacked > maxUnpackedTotal {
		return nil, fmt.Errorf("archives are too large when unpacked (max %d MB)", maxUnpackedTotal>>20)
	}
	return data, nil
}

// plainFile — обычный загруженный файл.
func plainFile(name string, data []byte) registryFile {
	return registryFile{Name: validUTF8(name), open: func() ([]byte, error) { return data, nil }}
}

// registrySkipReason: реестр — .txt; к какому счёту он относится (номер или код реестра в имени), решается позже,
// когда известны счета системы, поэтому любой .txt из архива считается кандидатом.
func registrySkipReason(base string) string {
	if !isTxt(base) {
		return ignoreNotTxt
	}
	return ""
}

// inflate распаковывает файл, который браузер сжал gzip перед отправкой (большие текстовые выписки иначе не укладываются
// в таймаут прокси на медленном канале); имя возвращается без «.gz». Файлы без суффикса .gz возвращаются как есть.
// Защита от «gzip-бомбы»: распакованное не больше limit.
func inflate(name string, data []byte, limit int64) (string, []byte, error) {
	if !strings.EqualFold(path.Ext(name), ".gz") {
		return name, data, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return name, nil, fmt.Errorf("not a valid gzip file")
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, limit+1))
	if err != nil {
		return name, nil, fmt.Errorf("not a valid gzip file")
	}
	if int64(len(out)) > limit {
		return name, nil, fmt.Errorf("file is too large when unpacked (max %d MB)", limit>>20)
	}
	return strings.TrimSuffix(name, name[len(name)-3:]), out, nil
}
