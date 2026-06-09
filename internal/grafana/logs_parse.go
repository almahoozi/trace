package grafana

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/almahoozi/trace/internal/config"
	"github.com/almahoozi/trace/internal/domain"
)

type lokiPayload struct {
	Data struct {
		Result []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func parseLokiPayload(body []byte, cfg config.LogsConfig) ([]domain.LogEntry, error) {
	var payload lokiPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse loki payload: %w", err)
	}

	entries := make([]domain.LogEntry, 0)
	for _, stream := range payload.Data.Result {
		for _, val := range stream.Values {
			if len(val) < 2 {
				continue
			}
			ts, line := val[0], val[1]
			entry := domain.LogEntry{
				Timestamp: parseUnixNano(ts),
				RawLine:   line,
				Service:   mapString(stream.Stream, cfg.ServiceField),
				Labels:    stream.Stream,
			}

			jsonMap := map[string]any{}
			if json.Unmarshal([]byte(line), &jsonMap) == nil {
				entry.JSON = jsonMap
				entry.Service = firstNonEmpty(entry.Service, stringFromMap(jsonMap, cfg.ServiceField), stringFromMap(jsonMap, "service.name"), mapString(stream.Stream, "service"), mapString(stream.Stream, "service_name"))
				entry.Level = normalizeLevel(firstNonEmpty(stringFromMap(jsonMap, cfg.LevelField), stringFromMap(jsonMap, "severity"), mapString(stream.Stream, cfg.LevelField), mapString(stream.Stream, "level")))
				entry.Message = firstNonEmpty(stringFromMap(jsonMap, cfg.MessageField), stringFromMap(jsonMap, "msg"), stringFromMap(jsonMap, "body"), mapString(stream.Stream, cfg.MessageField), line)
				if t := firstNonEmpty(stringFromMap(jsonMap, cfg.TimestampField), stringFromMap(jsonMap, "time"), mapString(stream.Stream, cfg.TimestampField)); t != "" {
					if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
						entry.Timestamp = parsed
					}
				}
			} else {
				entry.Message = line
				entry.Level = normalizeLevel(mapString(stream.Stream, cfg.LevelField))
			}

			if entry.Level == "" {
				entry.Level = "unknown"
			}
			if entry.Message == "" {
				entry.Message = strings.TrimSpace(line)
			}
			entries = append(entries, entry)
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})

	return entries, nil
}

func filterByLevel(entries []domain.LogEntry, threshold string, order []string) []domain.LogEntry {
	threshold = normalizeLevel(threshold)
	if threshold == "" {
		return entries
	}

	index := map[string]int{}
	for i, level := range order {
		index[normalizeLevel(level)] = i
	}
	min := index[threshold]

	filtered := make([]domain.LogEntry, 0, len(entries))
	for _, e := range entries {
		i, ok := index[normalizeLevel(e.Level)]
		if !ok {
			filtered = append(filtered, e)
			continue
		}
		if i >= min {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func normalizeLevel(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimPrefix(v, "level=")
	return v
}

func stringFromMap(m map[string]any, key string) string {
	if key == "" {
		return ""
	}
	if v, ok := m[key]; ok {
		return fmt.Sprint(v)
	}

	parts := strings.Split(key, ".")
	var cur any = m
	for _, part := range parts {
		obj, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		next, ok := obj[part]
		if !ok {
			return ""
		}
		cur = next
	}
	return fmt.Sprint(cur)
}

func mapString(m map[string]string, key string) string {
	if key == "" {
		return ""
	}
	if v, ok := m[key]; ok {
		return v
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func lokiRange(since string, traceStart, traceEnd time.Time) (start, end time.Time) {
	if !traceStart.IsZero() && !traceEnd.IsZero() {
		buffer := 5 * time.Minute
		if traceEnd.Before(traceStart) {
			traceStart, traceEnd = traceEnd, traceStart
		}
		return traceStart.Add(-buffer), traceEnd.Add(buffer)
	}
	end = time.Now()
	d, err := time.ParseDuration("-" + strings.TrimSpace(since))
	if err != nil {
		d = -60 * time.Minute
	}
	start = end.Add(d)
	return start, end
}
