package registryimport

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"testing"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func read(t *testing.T, f File) string {
	t.Helper()
	b, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestExpand(t *testing.T) {
	files, err := Expand("030626_VTB_231068_001.txt", []byte("data"))
	if err != nil || len(files) != 1 || files[0].Name != "030626_VTB_231068_001.txt" || read(t, files[0]) != "data" {
		t.Errorf("txt: %v %+v", err, files)
	}

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte("zipped"))
	zw.Close()
	files, err = Expand("r.txt.gz", gz.Bytes())
	if err != nil || len(files) != 1 || files[0].Name != "r.txt" || read(t, files[0]) != "zipped" {
		t.Errorf("gz: %v %+v", err, files)
	}
	if files, err := Expand("pic.png.gz", gz.Bytes()); err != nil || len(files) != 0 {
		t.Errorf("не-txt gz должен игнорироваться: %v %+v", err, files)
	}
	if _, err := Expand("r.txt.gz", []byte("not gzip")); err == nil {
		t.Error("битый gzip должен давать ошибку")
	}

	archive := zipOf(t, map[string]string{"a.txt": "A", "dir/b.TXT": "B", "pic.png": "x", "__MACOSX/._a.txt": "junk", "._c.txt": "junk"})
	files, err = Expand("reg.zip", archive)
	if err != nil || len(files) != 2 {
		t.Fatalf("zip: %v %+v", err, files)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Name] = read(t, f)
	}
	if got["reg.zip/a.txt"] != "A" || got["reg.zip/dir/b.TXT"] != "B" {
		t.Errorf("zip files: %v", got)
	}
	if _, err := Expand("bad.zip", []byte("nope")); err == nil {
		t.Error("битый zip должен давать ошибку")
	}

	if files, err := Expand("scan.pdf", []byte("x")); err != nil || len(files) != 0 {
		t.Errorf("pdf игнорируется: %v %+v", err, files)
	}
	if _, err := Expand("big.txt", make([]byte, MaxFileSize+1)); err == nil {
		t.Error("слишком большой файл должен давать ошибку")
	}
}
