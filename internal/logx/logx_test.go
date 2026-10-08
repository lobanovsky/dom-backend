package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewWritesToFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sub", "app.log")
	logger, closeLog := New(file)
	logger.Info("hello", "k", "v")
	closeLog()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"msg":"hello"`) || !strings.Contains(string(data), `"k":"v"`) {
		t.Errorf("log file content: %s", data)
	}
}

func TestNewFallsBackToStdout(t *testing.T) {
	logger, closeLog := New("/proc/definitely/not/writable/app.log")
	defer closeLog()
	logger.Info("still works")
	if logger, closeLog := New(""); logger == nil {
		t.Error("empty file name must give a stdout logger")
	} else {
		closeLog()
	}
}
