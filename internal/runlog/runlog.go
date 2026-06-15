package runlog

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

var (
	mu       sync.RWMutex
	logger   *slog.Logger
	logFile  *os.File
	logPath  string
	hasStart bool
	timings  []timingEntry
	runStart time.Time
)

type timingEntry struct {
	operation string
	duration  time.Duration
}

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
	timings = nil
	runStart = time.Now()
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
		logTimingSummaryLocked()
		if logger != nil && hasStart {
			logger.Info("run finished", "pid", os.Getpid(), "log_path", logPath)
		}
		_ = logFile.Close()
		logFile = nil
		logger = nil
		logPath = ""
		hasStart = false
		timings = nil
		runStart = time.Time{}
	}
}

func ObserveDuration(operation string, duration time.Duration) {
	op := operation
	if op == "" {
		op = "unknown"
	}

	mu.Lock()
	defer mu.Unlock()
	if logger == nil {
		return
	}
	timings = append(timings, timingEntry{operation: op, duration: duration})
}

func ObserveSinceRunStart(operation string) {
	mu.RLock()
	startedAt := runStart
	mu.RUnlock()
	if startedAt.IsZero() {
		return
	}
	ObserveDuration(operation, time.Since(startedAt))
}

func logTimingSummaryLocked() {
	if logger == nil {
		return
	}
	logger.Info("timings summary begin")
	if len(timings) == 0 {
		logger.Info("timings summary empty")
		logger.Info("timings summary end")
		return
	}

	type agg struct {
		count int
		total time.Duration
		max   time.Duration
	}
	aggs := map[string]agg{}
	for _, sample := range timings {
		current := aggs[sample.operation]
		current.count++
		current.total += sample.duration
		if sample.duration > current.max {
			current.max = sample.duration
		}
		aggs[sample.operation] = current
	}

	keys := make([]string, 0, len(aggs))
	for key := range aggs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		item := aggs[key]
		avg := time.Duration(0)
		if item.count > 0 {
			avg = item.total / time.Duration(item.count)
		}
		logger.Info(
			"timing",
			"operation", key,
			"count", item.count,
			"total_ms", item.total.Milliseconds(),
			"avg_ms", avg.Milliseconds(),
			"max_ms", item.max.Milliseconds(),
		)
	}
	logger.Info("timings summary end")
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
