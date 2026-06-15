package grafana

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/almahoozi/trace/internal/config"
	"github.com/almahoozi/trace/internal/domain"
	"github.com/almahoozi/trace/internal/runlog"
)

var ErrTraceNotFound = errors.New("trace not found")

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: httpClient,
	}
}

func (c *Client) FetchTrace(ctx context.Context, env config.Environment, traceID string) (*domain.Trace, error) {
	u := fmt.Sprintf("%s/api/datasources/proxy/uid/%s/api/traces/%s", c.baseURL, url.PathEscape(env.TempoDatasource), url.PathEscape(traceID))
	startedAt := time.Now()
	runlog.Debug("grafana fetch trace started", "environment", env.Name, "trace_id", traceID, "tempo_datasource_uid", env.TempoDatasource)

	body, statusCode, err := c.get(ctx, u)
	if err != nil {
		runlog.Warn("grafana fetch trace request failed", "environment", env.Name, "trace_id", traceID, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err)
		return nil, err
	}
	if statusCode == http.StatusNotFound {
		runlog.Debug("grafana fetch trace not found", "environment", env.Name, "trace_id", traceID, "duration_ms", time.Since(startedAt).Milliseconds())
		return nil, ErrTraceNotFound
	}
	if statusCode >= 400 {
		runlog.Warn("grafana fetch trace bad status", "environment", env.Name, "trace_id", traceID, "status_code", statusCode, "duration_ms", time.Since(startedAt).Milliseconds())
		return nil, fmt.Errorf("fetch trace failed with status %d", statusCode)
	}

	trace, err := parseTracePayload(traceID, body)
	if err != nil {
		dumpPath, dumpErr := dumpJSON("trace-payload-", body)
		if dumpErr != nil {
			runlog.Error("grafana trace parse failed and payload dump failed", "environment", env.Name, "trace_id", traceID, "parse_error", err, "dump_error", dumpErr)
			return nil, fmt.Errorf("%w (also failed to dump payload: %v)", err, dumpErr)
		}
		runlog.Error("grafana trace parse failed", "environment", env.Name, "trace_id", traceID, "payload_dump", dumpPath)
		return nil, fmt.Errorf("%w (payload dumped at %s)", err, dumpPath)
	}
	runlog.Info("grafana fetch trace succeeded", "environment", env.Name, "trace_id", traceID, "span_count", trace.SpanCount, "error_span_count", trace.ErrorSpanCount, "duration_ms", time.Since(startedAt).Milliseconds())
	trace.Environment = env.Name
	return trace, nil
}

func (c *Client) SearchTraces(ctx context.Context, env config.Environment, query string, limit int) ([]domain.TraceListItem, error) {
	runlog.Debug("grafana search traces started", "environment", env.Name, "query", strings.TrimSpace(query), "limit", limit)
	v := url.Values{}
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		v.Set("q", trimmed)
	}
	if limit <= 0 {
		limit = 50
	}
	v.Set("limit", strconv.Itoa(limit))

	u := fmt.Sprintf("%s/api/datasources/proxy/uid/%s/api/search?%s", c.baseURL, url.PathEscape(env.TempoDatasource), v.Encode())
	body, statusCode, err := c.get(ctx, u)
	if err != nil {
		runlog.Warn("grafana search traces request failed", "environment", env.Name, "error", err)
		return nil, err
	}
	if statusCode >= 400 {
		runlog.Warn("grafana search traces bad status", "environment", env.Name, "status_code", statusCode)
		return nil, fmt.Errorf("search traces failed with status %d", statusCode)
	}

	items, err := parseTraceSearchPayload(body)
	if err != nil {
		dumpPath, dumpErr := dumpJSON("trace-search-payload-", body)
		if dumpErr != nil {
			runlog.Error("grafana search parse failed and payload dump failed", "environment", env.Name, "parse_error", err, "dump_error", dumpErr)
			return nil, fmt.Errorf("%w (also failed to dump payload: %v)", err, dumpErr)
		}
		runlog.Error("grafana search parse failed", "environment", env.Name, "payload_dump", dumpPath)
		return nil, fmt.Errorf("%w (payload dumped at %s)", err, dumpPath)
	}
	runlog.Info("grafana search traces succeeded", "environment", env.Name, "trace_count", len(items), "limit", limit)
	return items, nil
}

func (c *Client) FetchLogs(ctx context.Context, cfg config.Config, env config.Environment, traceID string, traceStart, traceEnd time.Time) ([]domain.LogEntry, error) {
	return c.FetchLogsWithPadding(ctx, cfg, env, traceID, traceStart, traceEnd, 0)
}

func (c *Client) FetchLogsWithPadding(ctx context.Context, cfg config.Config, env config.Environment, traceID string, traceStart, traceEnd time.Time, padding time.Duration) ([]domain.LogEntry, error) {
	startedAt := time.Now()
	queryTemplate := strings.TrimSpace(env.LogQueryTemplate)
	if queryTemplate == "" {
		queryTemplate = strings.TrimSpace(cfg.Logs.QueryTemplate)
	}
	query := strings.ReplaceAll(queryTemplate, "{{trace_id}}", traceID)
	trimmedQuery := strings.TrimSpace(query)
	usedFallbackQuery := false
	if trimmedQuery == "" || strings.Contains(trimmedQuery, `{trace_id="`) {
		query = fmt.Sprintf(`{} |~ %q | json`, `"trace[-_]*id"\s*:\s*"`+traceID+`"`)
		usedFallbackQuery = true
	}

	start, end := lokiRange(cfg.Logs.Since, traceStart, traceEnd, padding)
	runlog.Debug(
		"grafana fetch logs started",
		"environment", env.Name,
		"trace_id", traceID,
		"loki_datasource_uid", env.LokiDatasource,
		"range_start", start.Format(time.RFC3339Nano),
		"range_end", end.Format(time.RFC3339Nano),
		"limit", max(1, cfg.Logs.Limit),
		"used_fallback_query", usedFallbackQuery,
	)
	u := fmt.Sprintf(
		"%s/api/datasources/proxy/uid/%s/loki/api/v1/query_range?query=%s&start=%s&end=%s&limit=%d&direction=forward",
		c.baseURL,
		url.PathEscape(env.LokiDatasource),
		url.QueryEscape(query),
		strconv.FormatInt(start.UnixNano(), 10),
		strconv.FormatInt(end.UnixNano(), 10),
		max(1, cfg.Logs.Limit),
	)

	body, statusCode, err := c.get(ctx, u)
	if err != nil {
		runlog.Warn("grafana fetch logs request failed", "environment", env.Name, "trace_id", traceID, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err)
		return nil, err
	}
	if statusCode >= 400 {
		runlog.Warn("grafana fetch logs bad status", "environment", env.Name, "trace_id", traceID, "status_code", statusCode, "duration_ms", time.Since(startedAt).Milliseconds())
		return nil, fmt.Errorf("fetch logs failed with status %d", statusCode)
	}

	entries, err := parseLokiPayload(body, cfg.Logs)
	if err != nil {
		dumpPath, dumpErr := dumpJSON("loki-payload-", body)
		if dumpErr != nil {
			runlog.Error("grafana logs parse failed and payload dump failed", "environment", env.Name, "trace_id", traceID, "parse_error", err, "dump_error", dumpErr)
			return nil, fmt.Errorf("%w (also failed to dump payload: %v)", err, dumpErr)
		}
		runlog.Error("grafana logs parse failed", "environment", env.Name, "trace_id", traceID, "payload_dump", dumpPath)
		return nil, fmt.Errorf("%w (payload dumped at %s)", err, dumpPath)
	}
	filtered := filterByLevel(entries, cfg.Logs.LevelThreshold, cfg.Logs.LevelOrder)
	runlog.Info("grafana fetch logs succeeded", "environment", env.Name, "trace_id", traceID, "entry_count", len(entries), "filtered_count", len(filtered), "duration_ms", time.Since(startedAt).Milliseconds(), "level_threshold", cfg.Logs.LevelThreshold)
	return filtered, nil
}

func (c *Client) get(ctx context.Context, requestURL string) ([]byte, int, error) {
	startedAt := time.Now()
	parsedURL, parseErr := url.Parse(requestURL)
	requestPath := ""
	queryLength := 0
	if parseErr == nil {
		requestPath = parsedURL.Path
		queryLength = len(parsedURL.RawQuery)
	}
	runlog.Debug("grafana http request started", "method", http.MethodGet, "path", requestPath, "query_length", queryLength)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		runlog.Error("failed to build grafana request", "error", err)
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		recordNetworkDuration(requestPath, startedAt)
		runlog.Warn("grafana http request failed", "method", http.MethodGet, "path", requestPath, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err)
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		recordNetworkDuration(requestPath, startedAt)
		runlog.Warn("failed to read grafana response body", "method", http.MethodGet, "path", requestPath, "status_code", resp.StatusCode, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err)
		return nil, 0, err
	}
	recordNetworkDuration(requestPath, startedAt)
	runlog.Debug("grafana http request completed", "method", http.MethodGet, "path", requestPath, "status_code", resp.StatusCode, "duration_ms", time.Since(startedAt).Milliseconds(), "response_bytes", len(body))

	if resp.StatusCode >= 400 {
		var e map[string]any
		if json.Unmarshal(body, &e) == nil {
			if msg, ok := e["message"].(string); ok && msg != "" {
				runlog.Warn("grafana error response", "method", http.MethodGet, "path", requestPath, "status_code", resp.StatusCode, "message", msg)
				return body, resp.StatusCode, fmt.Errorf("%s", msg)
			}
		}
	}
	return body, resp.StatusCode, nil
}

func recordNetworkDuration(requestPath string, startedAt time.Time) {
	duration := time.Since(startedAt)
	runlog.ObserveDuration("network.request_total", duration)
	if requestPath != "" {
		runlog.ObserveDuration("network.request.path."+requestPath, duration)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func dumpJSON(prefix string, body []byte) (string, error) {
	f, err := os.CreateTemp("", prefix+"*.json")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(body); err != nil {
		return "", err
	}
	return f.Name(), nil
}
