// Package logx настраивает журнал: JSON в stdout (docker logs) и, если задан файл, ещё и в файл с ротацией.
// Файл нужен, чтобы журнал не пропадал при пересоздании контейнера при деплое: каталог монтируется с хоста.
package logx

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// New возвращает логгер и функцию закрытия файла. Если файл открыть нельзя, журнал идёт только в stdout,
// а причина записывается первой строкой.
func New(file string) (*slog.Logger, func()) {
	out := io.Writer(os.Stdout)
	closeFn := func() {}
	var fileErr error
	if file != "" {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			fileErr = err
		} else if f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err != nil { // проверка прав до первой записи
			fileErr = err
		} else {
			_ = f.Close()
			rotator := &lumberjack.Logger{Filename: file, MaxSize: 20, MaxBackups: 10, MaxAge: 90, Compress: true}
			out = io.MultiWriter(os.Stdout, rotator)
			closeFn = func() { _ = rotator.Close() }
		}
	}
	logger := slog.New(slog.NewJSONHandler(out, nil))
	if fileErr != nil {
		logger.Error("log file is not writable, logging to stdout only", "file", file, "err", fileErr)
	} else if file != "" {
		logger.Info("logging to file", "file", file, "max_size_mb", 20, "max_backups", 10, "max_age_days", 90)
	}
	return logger, closeFn
}
