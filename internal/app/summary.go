package app

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/almahoozi/trace/internal/config"
	"github.com/almahoozi/trace/internal/domain"
)

func RenderTraceSummary(cfg config.Config, session *domain.Session) (string, error) {
	return RenderTraceSummaryWithColor(cfg, session, false)
}

func RenderTraceSummaryWithColor(cfg config.Config, session *domain.Session, color bool) (string, error) {
	if session == nil || session.Trace == nil {
		return "", fmt.Errorf("missing session trace data")
	}

	templateText := strings.TrimSpace(cfg.Output.TraceSummaryTemplate)
	if templateText == "" {
		templateText = strings.TrimSpace(config.DefaultTraceSummaryTemplate)
	} else if templateText == strings.TrimSpace(config.LegacyTraceSummaryTemplate) {
		templateText = strings.TrimSpace(config.DefaultTraceSummaryTemplate)
	}

	data := traceSummaryData(cfg, session)
	summary, err := renderSummaryTemplate(templateText, data, color)
	if err == nil {
		return strings.TrimSpace(summary), nil
	}

	defaultTemplate := strings.TrimSpace(config.DefaultTraceSummaryTemplate)
	if templateText == defaultTemplate {
		return "", fmt.Errorf("render default summary template: %w", err)
	}

	fallbackSummary, fallbackErr := renderSummaryTemplate(defaultTemplate, data, color)
	if fallbackErr != nil {
		return "", fmt.Errorf("render configured summary template: %w; fallback template failed: %v", err, fallbackErr)
	}
	return strings.TrimSpace(fallbackSummary), fmt.Errorf("render configured summary template: %w; printed default summary instead", err)
}

func renderSummaryTemplate(templateText string, data map[string]any, color bool) (string, error) {
	tmpl, err := template.New("trace-summary").Funcs(summaryTemplateFuncs(color)).Option("missingkey=error").Parse(templateText)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func traceSummaryData(cfg config.Config, session *domain.Session) map[string]any {
	trace := session.Trace
	loc := time.Local
	if cfgLoc, err := cfg.DisplayLocation(); err == nil {
		loc = cfgLoc
	}
	start := trace.StartTime.In(loc)
	end := start.Add(trace.Duration)

	operation := strings.TrimSpace(trace.OperationName)
	if operation == "" {
		operation = "-"
	}

	rootSpanAttributes := map[string]any{}
	if root := firstRootSpan(trace); root != nil && root.Attributes != nil {
		rootSpanAttributes = root.Attributes
	}
	httpStatusCode, hasHTTPStatus := extractHTTPStatusCode(rootSpanAttributes)
	httpStatusCodeText := ""
	if hasHTTPStatus {
		httpStatusCodeText = strconv.Itoa(httpStatusCode)
	}

	return map[string]any{
		"trace_id":             trace.TraceID,
		"environment":          session.Environment,
		"operation":            operation,
		"has_errors":           trace.ErrorSpanCount > 0,
		"http_status_code":     httpStatusCodeText,
		"has_http_status":      hasHTTPStatus,
		"duration":             trace.Duration,
		"duration_human":       trace.Duration.String(),
		"duration_ms":          trace.Duration.Milliseconds(),
		"duration_seconds":     trace.Duration.Seconds(),
		"duration_display":     formatSummaryDuration(trace.Duration),
		"start":                start,
		"end":                  end,
		"start_time":           start.Format("2006-01-02T15:04:05.000Z07:00"),
		"end_time":             end.Format("2006-01-02T15:04:05.000Z07:00"),
		"start_display":        start.Format("2006-01-02 15:04:05.000"),
		"end_display":          end.Format("2006-01-02 15:04:05.000"),
		"start_unix_ms":        trace.StartTime.UnixMilli(),
		"end_unix_ms":          end.UnixMilli(),
		"span_count":           trace.SpanCount,
		"error_span_count":     trace.ErrorSpanCount,
		"service_count":        trace.ServiceCount,
		"root_span_count":      len(trace.RootSpanIDs),
		"root_span_attributes": rootSpanAttributes,
		"log_count":            len(session.Logs),
		"grafana_url":          session.GrafanaURL,
		"betterstack_url":      session.BetterstackURL,
	}
}

func summaryTemplateFuncs(color bool) template.FuncMap {
	wrap := func(code string) func(string) string {
		return func(v string) string {
			if !color || v == "" {
				return v
			}
			return "\x1b[" + code + "m" + v + "\x1b[0m"
		}
	}

	return template.FuncMap{
		"gray":              wrap("90"),
		"light":             wrap("37"),
		"bright":            wrap("97"),
		"red":               wrap("31"),
		"duration_color":    durationColor(color),
		"http_status_color": httpStatusColor(color),
	}
}

func durationColor(color bool) func(any, string) string {
	paint := func(code, value string) string {
		if !color || value == "" {
			return value
		}
		return "\x1b[" + code + "m" + value + "\x1b[0m"
	}

	return func(rawDuration any, rendered string) string {
		duration, ok := rawDuration.(time.Duration)
		if !ok {
			return rendered
		}

		switch {
		case duration < 100*time.Millisecond:
			return paint("32", rendered)
		case duration < time.Second:
			return paint("97", rendered)
		case duration < 3*time.Second:
			return paint("33", rendered)
		default:
			return paint("31", rendered)
		}
	}
}

func httpStatusColor(color bool) func(any, string) string {
	paint := func(code, value string) string {
		if !color || value == "" {
			return value
		}
		return "\x1b[" + code + "m" + value + "\x1b[0m"
	}

	return func(rawStatus any, rendered string) string {
		statusCode, ok := parseHTTPStatusCode(rawStatus)
		if !ok {
			return paint("97", rendered)
		}

		switch statusCode / 100 {
		case 2:
			return paint("32", rendered)
		case 3:
			return paint("34", rendered)
		case 4:
			return paint("33", rendered)
		case 5:
			return paint("31", rendered)
		default:
			return paint("97", rendered)
		}
	}
}

func firstRootSpan(trace *domain.Trace) *domain.Span {
	if trace == nil {
		return nil
	}
	for _, rootID := range trace.RootSpanIDs {
		span := trace.SpansByID[rootID]
		if span != nil {
			return span
		}
	}
	for _, span := range trace.Spans {
		if span != nil && strings.TrimSpace(span.ParentID) == "" {
			return span
		}
	}
	if len(trace.Spans) > 0 {
		return trace.Spans[0]
	}
	return nil
}

func extractHTTPStatusCode(attrs map[string]any) (int, bool) {
	if attrs == nil {
		return 0, false
	}
	value, ok := attrs["http.response.status_code"]
	if !ok {
		return 0, false
	}
	return parseHTTPStatusCode(value)
}

func parseHTTPStatusCode(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, value > 0
	case int32:
		return int(value), value > 0
	case int64:
		return int(value), value > 0
	case uint:
		return int(value), value > 0
	case uint32:
		return int(value), value > 0
	case uint64:
		if value > 0 {
			return int(value), true
		}
		return 0, false
	case float64:
		if value <= 0 || math.Mod(value, 1) != 0 {
			return 0, false
		}
		return int(value), true
	case float32:
		f := float64(value)
		if f <= 0 || math.Mod(f, 1) != 0 {
			return 0, false
		}
		return int(f), true
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, false
		}
		if iv, err := strconv.Atoi(trimmed); err == nil {
			return iv, iv > 0
		}
		if fv, err := strconv.ParseFloat(trimmed, 64); err == nil {
			if fv > 0 && math.Mod(fv, 1) == 0 {
				return int(fv), true
			}
		}
		return 0, false
	default:
		return 0, false
	}
}

func formatSummaryDuration(d time.Duration) string {
	if d < time.Millisecond {
		micros := int64(math.Round(float64(d) / float64(time.Microsecond)))
		if micros < 1 {
			micros = 1
		}
		return strconv.FormatInt(micros, 10) + "us"
	}

	if d < 10*time.Second {
		ms := float64(d) / float64(time.Millisecond)
		return trimFloat(ms) + "ms"
	}

	seconds := float64(d) / float64(time.Second)
	return trimFloat(seconds) + "s"
}

func trimFloat(v float64) string {
	raw := strconv.FormatFloat(v, 'f', 2, 64)
	raw = strings.TrimRight(raw, "0")
	raw = strings.TrimRight(raw, ".")
	if raw == "" {
		return "0"
	}
	return raw
}
