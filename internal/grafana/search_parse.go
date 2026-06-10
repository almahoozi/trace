package grafana

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/almahoozi/trace/internal/domain"
)

func parseTraceSearchPayload(body []byte) ([]domain.TraceListItem, error) {
	var payload struct {
		Traces []map[string]any `json:"traces"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse trace search payload: %w", err)
	}

	items := make([]domain.TraceListItem, 0, len(payload.Traces))
	for _, trace := range payload.Traces {
		item := domain.TraceListItem{
			TraceID:       firstString(trace, "traceID", "traceId"),
			OperationName: firstString(trace, "rootTraceName", "rootSpanName", "operationName"),
			Service:       firstString(trace, "rootServiceName", "serviceName"),
			StartTime:     parseUnixNanoAny(firstValue(trace, "startTimeUnixNano", "startUnixNanos", "start")),
			Duration:      parseDurationFromAny(firstValue(trace, "durationNanos", "durationNs"), firstValue(trace, "durationMs", "duration_ms")),
			SpanCount:     parseInt(firstValue(trace, "spanCount", "span_count", "matchedSpans")),
		}
		item.ErrorSpanCount = parseInt(firstValue(trace, "errorCount", "errorSpanCount", "error_span_count"))

		if item.Service == "" || item.OperationName == "" {
			service, op := extractSpanSetDetails(firstValue(trace, "spanSets", "span_sets"))
			if item.Service == "" {
				item.Service = service
			}
			if item.OperationName == "" {
				item.OperationName = op
			}
		}

		if strings.TrimSpace(item.TraceID) == "" {
			continue
		}
		items = append(items, item)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].StartTime.Equal(items[j].StartTime) {
			return items[i].TraceID > items[j].TraceID
		}
		return items[i].StartTime.After(items[j].StartTime)
	})

	return items, nil
}

func extractSpanSetDetails(raw any) (service string, operation string) {
	sets, ok := raw.([]any)
	if !ok {
		return "", ""
	}
	for _, setRaw := range sets {
		setMap, ok := setRaw.(map[string]any)
		if !ok {
			continue
		}
		if service == "" {
			service = serviceFromAttributes(firstValue(setMap, "attributes"))
		}

		spans, _ := firstValue(setMap, "spans").([]any)
		for _, spanRaw := range spans {
			spanMap, ok := spanRaw.(map[string]any)
			if !ok {
				continue
			}
			if operation == "" {
				operation = firstString(spanMap, "name")
			}
			if service == "" {
				service = serviceFromAttributes(firstValue(spanMap, "attributes"))
			}
			if service != "" && operation != "" {
				return service, operation
			}
		}
	}
	return service, operation
}

func serviceFromAttributes(raw any) string {
	rawList, ok := raw.([]any)
	if !ok {
		return ""
	}

	attrs := make([]otlpAttribute, 0, len(rawList))
	for _, item := range rawList {
		b, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var attr otlpAttribute
		if err := json.Unmarshal(b, &attr); err != nil {
			continue
		}
		attrs = append(attrs, attr)
	}
	if len(attrs) == 0 {
		return ""
	}

	attrMap := attrsToMap(attrs)
	if svc, ok := attrMap["ctx.svc"].(string); ok && strings.TrimSpace(svc) != "" {
		return strings.TrimSpace(svc)
	}
	if svc, ok := attrMap["service.name"].(string); ok && strings.TrimSpace(svc) != "" {
		return strings.TrimSpace(svc)
	}
	return ""
}

func parseDurationFromAny(nsRaw any, msRaw any) time.Duration {
	if ns := parseInt64(nsRaw); ns > 0 {
		return time.Duration(ns)
	}
	if ms := parseFloat(msRaw); ms > 0 {
		return time.Duration(ms * float64(time.Millisecond))
	}
	return 0
}

func parseUnixNanoAny(v any) time.Time {
	n := parseInt64(v)
	if n <= 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

func parseInt(v any) int {
	return int(parseInt64(v))
}

func parseInt64(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int64:
		return t
	case float64:
		return int64(t)
	case json.Number:
		parsed, err := t.Int64()
		if err == nil {
			return parsed
		}
		f, err := t.Float64()
		if err != nil {
			return 0
		}
		return int64(f)
	case string:
		raw := strings.TrimSpace(t)
		if raw == "" {
			return 0
		}
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return parsed
		}
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
			return int64(parsed)
		}
	}
	return 0
}

func parseFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		parsed, err := t.Float64()
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func firstValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			s := strings.TrimSpace(fmt.Sprint(value))
			if s != "" {
				return s
			}
		}
	}
	return ""
}
