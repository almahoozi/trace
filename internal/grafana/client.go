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

	body, statusCode, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	if statusCode == http.StatusNotFound {
		return nil, ErrTraceNotFound
	}
	if statusCode >= 400 {
		return nil, fmt.Errorf("fetch trace failed with status %d", statusCode)
	}

	trace, err := parseTracePayload(traceID, body)
	if err != nil {
		dumpPath, dumpErr := dumpJSON("trace-payload-", body)
		if dumpErr != nil {
			return nil, fmt.Errorf("%w (also failed to dump payload: %v)", err, dumpErr)
		}
		return nil, fmt.Errorf("%w (payload dumped at %s)", err, dumpPath)
	}
	trace.Environment = env.Name
	return trace, nil
}

func (c *Client) SearchTraces(ctx context.Context, env config.Environment, query string, limit int) ([]domain.TraceListItem, error) {
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
		return nil, err
	}
	if statusCode >= 400 {
		return nil, fmt.Errorf("search traces failed with status %d", statusCode)
	}

	items, err := parseTraceSearchPayload(body)
	if err != nil {
		dumpPath, dumpErr := dumpJSON("trace-search-payload-", body)
		if dumpErr != nil {
			return nil, fmt.Errorf("%w (also failed to dump payload: %v)", err, dumpErr)
		}
		return nil, fmt.Errorf("%w (payload dumped at %s)", err, dumpPath)
	}
	return items, nil
}

func (c *Client) FetchLogs(ctx context.Context, cfg config.Config, env config.Environment, traceID string, traceStart, traceEnd time.Time) ([]domain.LogEntry, error) {
	query := strings.ReplaceAll(env.LogQueryTemplate, "{{trace_id}}", traceID)
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" || strings.Contains(trimmedQuery, `{trace_id="`) {
		query = fmt.Sprintf(`{} |~ %q | json`, `"trace[-_]*id"\s*:\s*"`+traceID+`"`)
	}

	start, end := lokiRange(cfg.Logs.Since, traceStart, traceEnd)
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
		return nil, err
	}
	if statusCode >= 400 {
		return nil, fmt.Errorf("fetch logs failed with status %d", statusCode)
	}

	entries, err := parseLokiPayload(body, cfg.Logs)
	if err != nil {
		dumpPath, dumpErr := dumpJSON("loki-payload-", body)
		if dumpErr != nil {
			return nil, fmt.Errorf("%w (also failed to dump payload: %v)", err, dumpErr)
		}
		return nil, fmt.Errorf("%w (payload dumped at %s)", err, dumpPath)
	}
	return filterByLevel(entries, cfg.Logs.LevelThreshold, cfg.Logs.LevelOrder), nil
}

func (c *Client) get(ctx context.Context, requestURL string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	if resp.StatusCode >= 400 {
		var e map[string]any
		if json.Unmarshal(body, &e) == nil {
			if msg, ok := e["message"].(string); ok && msg != "" {
				return body, resp.StatusCode, fmt.Errorf("%s", msg)
			}
		}
	}
	return body, resp.StatusCode, nil
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
