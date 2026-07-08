package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/almahoozi/trace/internal/domain"
)

const traceListCacheVersion = 1

type traceListCacheEntry struct {
	Version     int                    `json:"version"`
	SavedAt     time.Time              `json:"saved_at"`
	Environment string                 `json:"environment"`
	Query       string                 `json:"query"`
	Limit       int                    `json:"limit"`
	SPSS        int                    `json:"spss"`
	StartAt     time.Time              `json:"start_at"`
	EndAt       time.Time              `json:"end_at"`
	Items       []domain.TraceListItem `json:"items"`
}

func loadTraceListCache(environment, query string, limit, spss int, startAt, endAt time.Time) ([]domain.TraceListItem, bool, error) {
	if startAt.IsZero() || endAt.IsZero() {
		return nil, false, nil
	}
	path, err := traceListCachePath(environment, query, limit, spss, startAt, endAt)
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var entry traceListCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false, err
	}
	if entry.Version != traceListCacheVersion {
		return nil, false, nil
	}
	return append([]domain.TraceListItem(nil), entry.Items...), true, nil
}

func saveTraceListCache(environment, query string, limit, spss int, startAt, endAt time.Time, items []domain.TraceListItem) error {
	if startAt.IsZero() || endAt.IsZero() {
		return nil
	}
	path, err := traceListCachePath(environment, query, limit, spss, startAt, endAt)
	if err != nil {
		return err
	}
	entry := traceListCacheEntry{
		Version:     traceListCacheVersion,
		SavedAt:     time.Now().UTC(),
		Environment: strings.TrimSpace(environment),
		Query:       strings.TrimSpace(query),
		Limit:       limit,
		SPSS:        spss,
		StartAt:     startAt.UTC(),
		EndAt:       endAt.UTC(),
		Items:       append([]domain.TraceListItem(nil), items...),
	}
	buf, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return err
	}
	return nil
}

func traceListCachePath(environment, query string, limit, spss int, startAt, endAt time.Time) (string, error) {
	dir, err := SnapshotCacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	key := fmt.Sprintf("env=%s|q=%s|limit=%d|spss=%d|start=%d|end=%d", strings.ToLower(strings.TrimSpace(environment)), strings.TrimSpace(query), limit, spss, startAt.UTC().Unix(), endAt.UTC().Unix())
	hash := sha256.Sum256([]byte(key))
	name := "query-" + hex.EncodeToString(hash[:]) + ".json"
	return filepath.Join(dir, name), nil
}
