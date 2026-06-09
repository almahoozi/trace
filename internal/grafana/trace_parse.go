package grafana

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/almahoozi/trace/internal/domain"
)

type tracePayload struct {
	Batches []struct {
		Resource struct {
			Attributes []otlpAttribute `json:"attributes"`
		} `json:"resource"`
		InstrumentationLibrarySpans []struct {
			Spans []otlpSpan `json:"spans"`
		} `json:"instrumentationLibrarySpans"`
		ScopeSpans []struct {
			Spans []otlpSpan `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"batches"`
}

type otlpSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId"`
	Name              string          `json:"name"`
	Kind              string          `json:"kind"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	EndTimeUnixNano   string          `json:"endTimeUnixNano"`
	Attributes        []otlpAttribute `json:"attributes"`
	Events            []struct {
		Name         string          `json:"name"`
		TimeUnixNano string          `json:"timeUnixNano"`
		Attributes   []otlpAttribute `json:"attributes"`
	} `json:"events"`
	Links []struct {
		TraceID    string          `json:"traceId"`
		SpanID     string          `json:"spanId"`
		Attributes []otlpAttribute `json:"attributes"`
	} `json:"links"`
	Status struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
}

type otlpAttribute struct {
	Key   string `json:"key"`
	Value struct {
		StringValue any `json:"stringValue"`
		BoolValue   any `json:"boolValue"`
		IntValue    any `json:"intValue"`
		DoubleValue any `json:"doubleValue"`
	} `json:"value"`
}

func parseTracePayload(traceID string, body []byte) (*domain.Trace, error) {
	var p tracePayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("parse trace payload: %w", err)
	}

	serviceSet := map[string]struct{}{}
	spansByID := map[string]*domain.Span{}
	var spans []*domain.Span

	for _, batch := range p.Batches {
		resourceAttrs := attrsToMap(batch.Resource.Attributes)
		service, _ := resourceAttrs["service.name"].(string)

		for _, scope := range batch.InstrumentationLibrarySpans {
			for _, src := range scope.Spans {
				span := convertSpan(src, service)
				spans = append(spans, span)
				spansByID[span.ID] = span
				if span.Service != "" {
					serviceSet[span.Service] = struct{}{}
				}
			}
		}
		for _, scope := range batch.ScopeSpans {
			for _, src := range scope.Spans {
				span := convertSpan(src, service)
				spans = append(spans, span)
				spansByID[span.ID] = span
				if span.Service != "" {
					serviceSet[span.Service] = struct{}{}
				}
			}
		}
	}

	if len(spans) == 0 {
		return nil, fmt.Errorf("trace %s has no spans", traceID)
	}

	var roots []string
	var earliest time.Time
	var latest time.Time
	var errorSpans int

	for _, span := range spans {
		if span.HasError() {
			errorSpans++
		}
		if earliest.IsZero() || span.Start.Before(earliest) {
			earliest = span.Start
		}
		if latest.IsZero() || span.End.After(latest) {
			latest = span.End
		}
		if span.ParentID == "" || spansByID[span.ParentID] == nil {
			roots = append(roots, span.ID)
			continue
		}
		spansByID[span.ParentID].Children = append(spansByID[span.ParentID].Children, span)
	}

	for _, span := range spans {
		sort.Slice(span.Children, func(i, j int) bool {
			return span.Children[i].Start.Before(span.Children[j].Start)
		})
	}

	operation := spans[0].Name
	if len(roots) > 0 {
		if root := spansByID[roots[0]]; root != nil {
			operation = root.Name
		}
	}
	return &domain.Trace{
		TraceID:        traceID,
		RootSpanIDs:    roots,
		Spans:          spans,
		SpansByID:      spansByID,
		OperationName:  operation,
		Duration:       latest.Sub(earliest),
		StartTime:      earliest,
		ServiceCount:   len(serviceSet),
		ErrorSpanCount: errorSpans,
		SpanCount:      len(spans),
	}, nil
}

func convertSpan(src otlpSpan, defaultService string) *domain.Span {
	attrs := attrsToMap(src.Attributes)
	service := defaultService
	if v, ok := attrs["service.name"].(string); ok && v != "" {
		service = v
	}
	events := make([]domain.SpanEvent, 0, len(src.Events))
	for _, ev := range src.Events {
		events = append(events, domain.SpanEvent{
			Name:       ev.Name,
			Time:       parseUnixNano(ev.TimeUnixNano),
			Attributes: attrsToMap(ev.Attributes),
		})
	}
	links := make([]domain.SpanLink, 0, len(src.Links))
	for _, link := range src.Links {
		links = append(links, domain.SpanLink{
			TraceID:    normalizeID(link.TraceID),
			SpanID:     normalizeID(link.SpanID),
			Attributes: attrsToMap(link.Attributes),
		})
	}

	spanID := normalizeID(src.SpanID)
	parentID := normalizeID(src.ParentSpanID)

	start := parseUnixNano(src.StartTimeUnixNano)
	end := parseUnixNano(src.EndTimeUnixNano)

	return &domain.Span{
		ID:         spanID,
		ParentID:   parentID,
		Service:    service,
		Name:       src.Name,
		Kind:       normalizeKind(src.Kind),
		Start:      start,
		End:        end,
		Duration:   end.Sub(start),
		StatusCode: src.Status.Code,
		StatusMsg:  src.Status.Message,
		Attributes: attrs,
		Events:     events,
		Links:      links,
	}
}

func normalizeKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "internal"
	}
	return strings.ToLower(kind)
}

func parseUnixNano(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	n, err := time.ParseDuration(raw + "ns")
	if err == nil {
		return time.Unix(0, n.Nanoseconds())
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed
	}
	return time.Time{}
}

func attrsToMap(attributes []otlpAttribute) map[string]any {
	res := make(map[string]any, len(attributes))
	for _, attr := range attributes {
		if v, ok := anyString(attr.Value.StringValue); ok {
			res[attr.Key] = v
			continue
		}
		if v, ok := anyBool(attr.Value.BoolValue); ok {
			res[attr.Key] = v
			continue
		}
		if v, ok := anyInt64(attr.Value.IntValue); ok {
			res[attr.Key] = v
			continue
		}
		if v, ok := anyFloat64(attr.Value.DoubleValue); ok {
			res[attr.Key] = v
			continue
		}
		res[attr.Key] = ""
	}
	return res
}

func anyString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	default:
		return "", false
	}
}

func anyBool(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(t))
		if err != nil {
			return false, false
		}
		return parsed, true
	default:
		return false, false
	}
}

func anyInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case int64:
		return t, true
	case int:
		return int64(t), true
	case json.Number:
		parsed, err := t.Int64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func anyFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case json.Number:
		parsed, err := t.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func normalizeID(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if decoded, err := base64.StdEncoding.DecodeString(v); err == nil && len(decoded) > 0 {
		return fmt.Sprintf("%x", decoded)
	}
	return v
}
