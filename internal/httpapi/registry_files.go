package httpapi

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"
)

// Пределы загрузки реестров. Файлы читаются в память, на диск ничего не пишется.
const (
	maxRegistrySize   = 5 << 20   // один файл реестра
	maxArchiveSize    = 50 << 20  // сам zip-архив
	maxUnpackedTotal  = 200 << 20 // всё распакованное за один запрос
	maxArchiveEntries = 2000
	maxRequestSize    = 100 << 20
)

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

func isZip(name string, data []byte) bool {
	return strings.EqualFold(path.Ext(name), ".zip") || bytes.HasPrefix(data, []byte("PK\x03\x04"))
}

func validUTF8(s string) string { return strings.ToValidUTF8(s, "?") }

// expandZip возвращает файлы архива. Служебные записи macOS (__MACOSX/, ._имя) и каталоги пропускаются.
// Вложенные архивы не раскрываются. unpacked — общий счётчик распакованных байт на запрос.
func expandZip(archiveName string, data []byte, unpacked *int64) ([]registryFile, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a valid zip archive")
	}
	if len(zr.File) > maxArchiveEntries {
		return nil, fmt.Errorf("archive has too many entries (max %d)", maxArchiveEntries)
	}
	var files []registryFile
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if f.FileInfo().IsDir() || strings.HasPrefix(name, "__MACOSX/") || strings.Contains(name, "/__MACOSX/") || strings.HasPrefix(path.Base(name), "._") {
			continue
		}
		f := f
		files = append(files, registryFile{
			Name: validUTF8(archiveName + "/" + name),
			open: func() ([]byte, error) { return readZipEntry(f, unpacked) },
		})
	}
	return files, nil
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
