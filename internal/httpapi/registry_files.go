package httpapi

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"

	"dom-backend/internal/sberregistry"
)

// Пределы загрузки реестров. Файлы читаются в память, на диск ничего не пишется.
const (
	maxRegistrySize  = 5 << 20   // один файл реестра
	maxArchiveSize   = 50 << 20  // сам zip-архив
	maxUnpackedTotal = 200 << 20 // всё распакованное за один запрос
	maxRequestSize   = 100 << 20
)

// maxArchiveEntries — предел числа записей в архиве. Он почти не защищает: размер архива и так ограничен
// (maxArchiveSize), а распаковываются только подходящие по имени файлы. Переменная, чтобы тест мог её уменьшить.
var maxArchiveEntries = 1_000_000

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

// expandZip возвращает файлы архива, подходящие под accept(basename) (например, .txt с номером счёта в имени); остальные
// считаются в ignored и сразу отбрасываются, чтобы не держать в памяти список всех записей большого архива.
// Служебные записи macOS (__MACOSX/, ._имя) и каталоги пропускаются без счёта. Вложенные архивы не раскрываются.
// unpacked — общий счётчик распакованных байт на запрос.
func expandZip(archiveName string, data []byte, unpacked *int64, accept func(base string) bool) (files []registryFile, ignored int, err error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, 0, fmt.Errorf("not a valid zip archive")
	}
	if len(zr.File) > maxArchiveEntries {
		return nil, 0, fmt.Errorf("archive has too many entries (max %d)", maxArchiveEntries)
	}
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if f.FileInfo().IsDir() || strings.HasPrefix(name, "__MACOSX/") || strings.Contains(name, "/__MACOSX/") || strings.HasPrefix(path.Base(name), "._") {
			continue
		}
		if !accept(path.Base(name)) {
			ignored++
			continue
		}
		f := f
		files = append(files, registryFile{
			Name: validUTF8(archiveName + "/" + name),
			open: func() ([]byte, error) { return readZipEntry(f, unpacked) },
		})
	}
	return files, ignored, nil
}

// readZipEntry читает запись с ограничением по фактическому объёму: заголовок архива может лгать о размере.
func readZipEntry(f *zip.File, unpacked *int64) ([]byte, error) {
	if f.UncompressedSize64 > maxRegistrySize {
		return nil, fmt.Errorf("file is too large (max %d MB)", maxRegistrySize>>20)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("cannot read the archive entry")
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxRegistrySize+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read the archive entry")
	}
	if len(data) > maxRegistrySize {
		return nil, fmt.Errorf("file is too large (max %d MB)", maxRegistrySize>>20)
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

// registryName: реестр Сбера — .txt с 20-значным номером счёта в имени.
func registryName(base string) bool {
	return isTxt(base) && len(sberregistry.AccountsInName(base)) > 0
}
