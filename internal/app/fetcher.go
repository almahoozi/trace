package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/almahoozi/trace/internal/config"
	"github.com/almahoozi/trace/internal/domain"
	"github.com/almahoozi/trace/internal/grafana"
	"github.com/almahoozi/trace/internal/runlog"
	"golang.org/x/sync/errgroup"
)

var ErrTraceNotFound = errors.New("trace not found")
var ErrEnvironmentNotFound = errors.New("environment not found")

type Fetcher struct {
	client *grafana.Client
}

func NewFetcher(client *grafana.Client) *Fetcher {
	return &Fetcher{client: client}
}

func (f *Fetcher) FetchTraceSession(ctx context.Context, cfg config.Config, traceID string) (*domain.Session, error) {
	sessionStartedAt := time.Now()
	defer func() {
		runlog.ObserveDuration("concurrent_group.fetch_trace_session_total", time.Since(sessionStartedAt))
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type logsResult struct {
		entries []domain.LogEntry
		err     error
	}
	type traceMatch struct {
		env      config.Environment
		trace    *domain.Trace
		prefetch chan logsResult
	}

	var (
		mu      sync.Mutex
		found   *traceMatch
		lastErr error
	)

	envLookupWallStartedAt := time.Now()
	logsPrefetchWallStartedAt := time.Now()
	var logsPrefetchWG sync.WaitGroup

	g, egCtx := errgroup.WithContext(ctx)
	for _, env := range cfg.Environments {
		env := env
		logsPrefetchWG.Add(1)
		g.Go(func() error {
			envLookupStartedAt := time.Now()
			defer func() {
				runlog.ObserveDuration("concurrent_group.fetch_trace_session_env_lookup", time.Since(envLookupStartedAt))
			}()

			runlog.Debug("querying environment for trace", "environment", env.Name, "trace_id", traceID)
			logsCh := make(chan logsResult, 1)
			go func() {
				defer logsPrefetchWG.Done()
				prefetchStartedAt := time.Now()
				defer func() {
					runlog.ObserveDuration("concurrent_group.fetch_trace_session_logs_prefetch", time.Since(prefetchStartedAt))
				}()

				logsCtx, logsCancel := context.WithTimeout(context.Background(), cfg.Grafana.Timeout()+5*time.Second)
				defer logsCancel()
				startedAt := time.Now()
				entries, err := f.client.FetchLogs(logsCtx, cfg, env, traceID, time.Time{}, time.Time{})
				if err != nil {
					runlog.Warn("prefetch logs failed", "environment", env.Name, "trace_id", traceID, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err)
				} else {
					runlog.Debug("prefetch logs succeeded", "environment", env.Name, "trace_id", traceID, "entry_count", len(entries), "duration_ms", time.Since(startedAt).Milliseconds())
				}
				logsCh <- logsResult{entries: entries, err: err}
			}()

			trace, err := f.client.FetchTrace(egCtx, env, traceID)
			if err != nil {
				if errors.Is(err, grafana.ErrTraceNotFound) || errors.Is(err, context.Canceled) {
					runlog.Debug("trace not found in environment", "environment", env.Name, "trace_id", traceID)
					return nil
				}
				mu.Lock()
				lastErr = fmt.Errorf("%s: %w", env.Name, err)
				mu.Unlock()
				runlog.Warn("fetch trace failed in environment", "environment", env.Name, "trace_id", traceID, "error", err)
				return nil
			}

			mu.Lock()
			if found == nil {
				found = &traceMatch{env: env, trace: trace, prefetch: logsCh}
				runlog.Info("trace matched environment", "environment", env.Name, "trace_id", traceID, "span_count", trace.SpanCount)
				cancel()
			}
			mu.Unlock()
			return nil
		})
	}
	go func() {
		logsPrefetchWG.Wait()
		runlog.ObserveDuration("concurrent_group.fetch_trace_session_logs_prefetch_wall", time.Since(logsPrefetchWallStartedAt))
	}()

	_ = g.Wait()
	runlog.ObserveDuration("concurrent_group.fetch_trace_session_env_lookup_wall", time.Since(envLookupWallStartedAt))

	if found == nil {
		if lastErr != nil {
			runlog.Error("trace lookup failed", "trace_id", traceID, "last_error", lastErr)
			return nil, lastErr
		}
		runlog.Warn("trace lookup yielded no matches", "trace_id", traceID)
		return nil, ErrTraceNotFound
	}

	prefetched := []domain.LogEntry(nil)
	if found.prefetch != nil {
		select {
		case res := <-found.prefetch:
			if res.err == nil {
				prefetched = res.entries
				runlog.Debug("using prefetched logs", "environment", found.env.Name, "trace_id", traceID, "entry_count", len(prefetched))
			} else {
				runlog.Warn("prefetched logs unavailable", "environment", found.env.Name, "trace_id", traceID, "error", res.err)
			}
		case <-time.After(250 * time.Millisecond):
			runlog.Warn("prefetched logs timed out", "environment", found.env.Name, "trace_id", traceID, "wait_ms", 250)
		}
	}

	logs := prefetched
	if len(logs) == 0 {
		finalLogsStartedAt := time.Now()
		logsCtx, logsCancel := context.WithTimeout(context.Background(), cfg.Grafana.Timeout()+5*time.Second)
		defer logsCancel()
		fetched, err := f.client.FetchLogs(logsCtx, cfg, found.env, traceID, found.trace.StartTime, found.trace.StartTime.Add(found.trace.Duration))
		runlog.ObserveDuration("concurrent_group.fetch_trace_session_logs_final", time.Since(finalLogsStartedAt))
		if err == nil {
			logs = fetched
			runlog.Info("fetched logs in trace window", "environment", found.env.Name, "trace_id", traceID, "entry_count", len(logs))
		} else {
			runlog.Warn("failed fetching logs in trace window", "environment", found.env.Name, "trace_id", traceID, "error", err)
		}
	}

	grafanaURL := renderTemplate(cfg.URLs.GrafanaTraceTemplate, map[string]string{
		"base_url":             cfg.Grafana.BaseURL,
		"trace_id":             traceID,
		"env":                  found.env.Name,
		"tempo_datasource_uid": found.env.TempoDatasource,
	})
	if shouldAutoBuildGrafanaURL(cfg.URLs.GrafanaTraceTemplate) {
		grafanaURL = buildGrafanaTraceURL(cfg.Grafana.BaseURL, found.env.TempoDatasource, traceID)
	}
	betterstackURL := renderTemplate(cfg.URLs.BetterstackLogTemplate, map[string]string{
		"trace_id":              traceID,
		"env":                   found.env.Name,
		"betterstack_source_id": found.env.BetterstackID,
	})

	found.trace.GrafanaExternalURL = grafanaURL
	return &domain.Session{
		Trace:          found.trace,
		Logs:           logs,
		Environment:    found.env.Name,
		GrafanaURL:     grafanaURL,
		BetterstackURL: betterstackURL,
	}, nil
}

func (f *Fetcher) FetchTraceSessionInEnvironment(ctx context.Context, cfg config.Config, envName, traceID string) (*domain.Session, error) {
	env, ok := findEnvironment(cfg, envName)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrEnvironmentNotFound, envName)
	}

	trace, err := f.client.FetchTrace(ctx, env, traceID)
	if err != nil {
		if errors.Is(err, grafana.ErrTraceNotFound) {
			runlog.Warn("trace not found in explicit environment", "environment", envName, "trace_id", traceID)
			return nil, ErrTraceNotFound
		}
		runlog.Error("failed to fetch trace in explicit environment", "environment", envName, "trace_id", traceID, "error", err)
		return nil, err
	}

	logsCtx, logsCancel := context.WithTimeout(context.Background(), cfg.Grafana.Timeout()+5*time.Second)
	defer logsCancel()
	logs, err := f.client.FetchLogs(logsCtx, cfg, env, traceID, trace.StartTime, trace.StartTime.Add(trace.Duration))
	if err != nil {
		runlog.Warn("failed to fetch logs in explicit environment", "environment", envName, "trace_id", traceID, "error", err)
		logs = nil
	} else {
		runlog.Info("fetched logs in explicit environment", "environment", envName, "trace_id", traceID, "entry_count", len(logs))
	}

	grafanaURL := renderTemplate(cfg.URLs.GrafanaTraceTemplate, map[string]string{
		"base_url":             cfg.Grafana.BaseURL,
		"trace_id":             traceID,
		"env":                  env.Name,
		"tempo_datasource_uid": env.TempoDatasource,
	})
	if shouldAutoBuildGrafanaURL(cfg.URLs.GrafanaTraceTemplate) {
		grafanaURL = buildGrafanaTraceURL(cfg.Grafana.BaseURL, env.TempoDatasource, traceID)
	}
	betterstackURL := renderTemplate(cfg.URLs.BetterstackLogTemplate, map[string]string{
		"trace_id":              traceID,
		"env":                   env.Name,
		"betterstack_source_id": env.BetterstackID,
	})

	trace.GrafanaExternalURL = grafanaURL
	return &domain.Session{
		Trace:          trace,
		Logs:           logs,
		Environment:    env.Name,
		GrafanaURL:     grafanaURL,
		BetterstackURL: betterstackURL,
	}, nil
}

func (f *Fetcher) FetchTraceList(ctx context.Context, cfg config.Config, envName, query string, limit int) ([]domain.TraceListItem, error) {
	env, ok := findEnvironment(cfg, envName)
	if !ok {
		runlog.Warn("environment not found for browse", "environment", envName)
		return nil, fmt.Errorf("%w: %s", ErrEnvironmentNotFound, envName)
	}
	runlog.Info("fetching trace list", "environment", env.Name, "query", strings.TrimSpace(query), "limit", limit)

	items, err := f.client.SearchTraces(ctx, env, query, limit)
	if err != nil {
		runlog.Error("failed searching traces", "environment", env.Name, "error", err)
		return nil, err
	}
	if len(items) == 0 {
		runlog.Info("no traces returned from search", "environment", env.Name, "query", strings.TrimSpace(query))
		return items, nil
	}

	enriched := make([]domain.TraceListItem, len(items))
	copy(enriched, items)
	var mu sync.Mutex
	g, egCtx := errgroup.WithContext(ctx)
	g.SetLimit(4)

	for i := range enriched {
		i := i
		if strings.TrimSpace(enriched[i].TraceID) == "" {
			continue
		}
		g.Go(func() error {
			trace, err := f.client.FetchTrace(egCtx, env, enriched[i].TraceID)
			if err != nil {
				return nil
			}

			service := primaryService(trace)
			mu.Lock()
			enriched[i].OperationName = firstNonEmpty(trace.OperationName, enriched[i].OperationName)
			enriched[i].Service = firstNonEmpty(service, enriched[i].Service)
			enriched[i].SpanCount = trace.SpanCount
			enriched[i].ErrorSpanCount = trace.ErrorSpanCount
			enriched[i].Duration = trace.Duration
			if enriched[i].StartTime.IsZero() {
				enriched[i].StartTime = trace.StartTime
			}
			mu.Unlock()
			return nil
		})
	}

	_ = g.Wait()
	runlog.Info("trace list enrichment completed", "environment", env.Name, "trace_count", len(enriched))
	return enriched, nil
}

func findEnvironment(cfg config.Config, name string) (config.Environment, bool) {
	target := strings.TrimSpace(name)
	for _, env := range cfg.Environments {
		if strings.EqualFold(strings.TrimSpace(env.Name), target) {
			return env, true
		}
	}
	return config.Environment{}, false
}

func primaryService(trace *domain.Trace) string {
	if trace == nil {
		return ""
	}
	for _, rootID := range trace.RootSpanIDs {
		span := trace.SpansByID[rootID]
		if span == nil {
			continue
		}
		if value, ok := span.Attributes["ctx.svc"]; ok {
			svc := strings.TrimSpace(fmt.Sprint(value))
			if svc != "" {
				return svc
			}
		}
		if svc := strings.TrimSpace(span.Service); svc != "" {
			return svc
		}
	}
	for _, span := range trace.Spans {
		if span == nil {
			continue
		}
		if value, ok := span.Attributes["ctx.svc"]; ok {
			svc := strings.TrimSpace(fmt.Sprint(value))
			if svc != "" {
				return svc
			}
		}
		if svc := strings.TrimSpace(span.Service); svc != "" {
			return svc
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func renderTemplate(template string, values map[string]string) string {
	res := template
	for key, value := range values {
		res = strings.ReplaceAll(res, "{{"+key+"}}", value)
	}
	return res
}

func shouldAutoBuildGrafanaURL(template string) bool {
	trimmed := strings.TrimSpace(template)
	if trimmed == "" {
		return true
	}
	return trimmed == "{{base_url}}/explore?traceId={{trace_id}}&env={{env}}"
}

func buildGrafanaTraceURL(baseURL, tempoUID, traceID string) string {
	panes := map[string]any{
		"A": map[string]any{
			"datasource": tempoUID,
			"queries": []map[string]any{
				{
					"refId": "A",
					"datasource": map[string]any{
						"type": "tempo",
						"uid":  tempoUID,
					},
					"queryType":                     "traceql",
					"limit":                         20,
					"tableType":                     "traces",
					"metricsQueryType":              "range",
					"serviceMapUseNativeHistograms": false,
					"query":                         traceID,
				},
			},
			"range": map[string]any{
				"from": "now-1h",
				"to":   "now",
			},
			"compact": false,
		},
	}
	panesJSON, _ := json.Marshal(panes)
	v := url.Values{}
	v.Set("schemaVersion", "1")
	v.Set("panes", string(panesJSON))
	v.Set("orgId", "1")
	return strings.TrimRight(baseURL, "/") + "/explore?" + v.Encode()
}
