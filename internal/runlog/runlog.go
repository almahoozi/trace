package runlog

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	mu       sync.RWMutex
	logger   *slog.Logger
	logFile  *os.File
	logPath  string
	hasStart bool
)

func Start(path string) error {
	if path == "" {
		return fmt.Errorf("log path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()
	if logFile != nil {
		_ = logFile.Close()
	}
	logFile = f
	logPath = path
	handler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger = slog.New(handler)
	hasStart = true
	logger.Info(
		"run started",
		"pid", os.Getpid(),
		"log_path", path,
		"cwd", mustGetwd(),
		"goos", runtime.GOOS,
		"goarch", runtime.GOARCH,
	)
	return nil
}

func Close() {
	mu.Lock()
	defer mu.Unlock()
	if logFile != nil {
		if logger != nil && hasStart {
			logger.Info("run finished", "pid", os.Getpid(), "log_path", logPath)
		}
		_ = logFile.Close()
		logFile = nil
		logger = nil
		logPath = ""
		hasStart = false
	}
}

func Debug(msg string, args ...any) {
	mu.RLock()
	l := logger
	mu.RUnlock()
	if l == nil {
		return
	}
	l.Debug(msg, args...)
}

func Info(msg string, args ...any) {
	mu.RLock()
	l := logger
	mu.RUnlock()
	if l == nil {
		return
	}
	l.Info(msg, args...)
}

func Warn(msg string, args ...any) {
	mu.RLock()
	l := logger
	mu.RUnlock()
	if l == nil {
		return
	}
	l.Warn(msg, args...)
}

func Error(msg string, args ...any) {
	mu.RLock()
	l := logger
	mu.RUnlock()
	if l == nil {
		return
	}
	l.Error(msg, args...)
}

func Printf(format string, args ...any) {
	Info(fmt.Sprintf(format, args...))
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}
