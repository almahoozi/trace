package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/almahoozi/trace/internal/domain"
)

const SnapshotVersion = 1

type Snapshot struct {
	Version    int                `json:"version"`
	SavedAt    time.Time          `json:"saved_at"`
	Session    snapshotSession    `json:"session"`
	ServiceMap snapshotServiceMap `json:"service_map"`
}

type snapshotSession struct {
	Environment    string            `json:"environment"`
	GrafanaURL     string            `json:"grafana_url"`
	BetterstackURL string            `json:"betterstack_url"`
	Trace          snapshotTrace     `json:"trace"`
	Logs           []domain.LogEntry `json:"logs"`
}

type snapshotTrace struct {
	TraceID            string         `json:"trace_id"`
	RootSpanIDs        []string       `json:"root_span_ids"`
	Spans              []snapshotSpan `json:"spans"`
	OperationName      string         `json:"operation_name"`
	Duration           time.Duration  `json:"duration"`
	StartTime          time.Time      `json:"start_time"`
	ServiceCount       int            `json:"service_count"`
	ErrorSpanCount     int            `json:"error_span_count"`
	SpanCount          int            `json:"span_count"`
	Environment        string         `json:"environment"`
	GrafanaExternalURL string         `json:"grafana_external_url"`
}

type snapshotSpan struct {
	ID         string             `json:"id"`
	ParentID   string             `json:"parent_id"`
	Service    string             `json:"service"`
	Name       string             `json:"name"`
	Kind       string             `json:"kind"`
	Start      time.Time          `json:"start"`
	End        time.Time          `json:"end"`
	Duration   time.Duration      `json:"duration"`
	XCost      time.Duration      `json:"x_cost"`
	StatusCode string             `json:"status_code"`
	StatusMsg  string             `json:"status_msg"`
	Attributes map[string]any     `json:"attributes"`
	Events     []domain.SpanEvent `json:"events"`
	Links      []domain.SpanLink  `json:"links"`
}

type snapshotServiceMap struct {
	TotalRequestCost string                    `json:"total_request_cost"`
	Nodes            []snapshotServiceNode     `json:"nodes"`
	Edges            []snapshotServiceEdge     `json:"edges"`
	External         []snapshotServiceExternal `json:"external"`
}

type snapshotServiceNode struct {
	Name  string `json:"name"`
	Proxy bool   `json:"proxy"`
	Cost  string `json:"cost"`
}

type snapshotServiceEdge struct {
	From      string `json:"from"`
	FromProxy string `json:"from_proxy,omitempty"`
	To        string `json:"to"`
	ToProxy   string `json:"to_proxy,omitempty"`
	Calls     int    `json:"calls"`
}

type snapshotServiceExternal struct {
	From      string `json:"from"`
	FromProxy string `json:"from_proxy,omitempty"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Cost      string `json:"cost"`
	Calls     int    `json:"calls"`
}

func SaveSessionSnapshot(path string, session *domain.Session) error {
	if session == nil || session.Trace == nil {
		return fmt.Errorf("nil session")
	}
	snap := Snapshot{
		Version:    SnapshotVersion,
		SavedAt:    time.Now().UTC(),
		Session:    toSnapshotSession(session),
		ServiceMap: buildSnapshotServiceMap(session.Trace),
	}
	buf, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(buf, '\n'), 0o644)
}

func SnapshotCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "trace", "snapshots"), nil
}

func DefaultSnapshotPath(traceID string) (string, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return "", fmt.Errorf("empty trace id")
	}
	dir, err := SnapshotCacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, traceID+".json"), nil
}

func ResolveSnapshotOpenPath(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("empty snapshot path")
	}
	if p := existingSnapshotPathCandidate(input); p != "" {
		return p, nil
	}
	if p := existingSnapshotPathCandidate(input + ".json"); p != "" {
		return p, nil
	}
	dir, err := SnapshotCacheDir()
	if err != nil {
		return "", err
	}
	if p := existingSnapshotPathCandidate(filepath.Join(dir, input)); p != "" {
		return p, nil
	}
	if p := existingSnapshotPathCandidate(filepath.Join(dir, input+".json")); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("snapshot not found: %s", input)
}

func existingSnapshotPathCandidate(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

func CleanupSnapshotCache(ctx context.Context, maxBytes, targetBytes int64) error {
	if maxBytes <= 0 {
		return nil
	}
	if targetBytes <= 0 {
		targetBytes = maxBytes / 10
	}
	if targetBytes >= maxBytes {
		targetBytes = maxBytes / 10
	}
	if targetBytes < 0 {
		targetBytes = 0
	}

	dir, err := SnapshotCacheDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	type cacheFile struct {
		path    string
		size    int64
		modTime time.Time
	}

	files := make([]cacheFile, 0, len(entries))
	totalSize := int64(0)
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		size := info.Size()
		totalSize += size
		files = append(files, cacheFile{
			path:    filepath.Join(dir, entry.Name()),
			size:    size,
			modTime: info.ModTime(),
		})
	}

	if totalSize <= maxBytes {
		return nil
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	for _, file := range files {
		if totalSize <= targetBytes {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if removeErr := os.Remove(file.path); removeErr != nil {
			continue
		}
		totalSize -= file.size
	}

	return nil
}

func LoadSessionSnapshot(path string) (*domain.Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("invalid snapshot json: %w", err)
	}
	if snap.Version == 0 {
		return nil, fmt.Errorf("snapshot missing version")
	}
	return fromSnapshotSession(snap.Session), nil
}

func toSnapshotSession(session *domain.Session) snapshotSession {
	trace := session.Trace
	spans := make([]snapshotSpan, 0, len(trace.Spans))
	for _, span := range trace.Spans {
		if span == nil {
			continue
		}
		spans = append(spans, snapshotSpan{
			ID:         span.ID,
			ParentID:   span.ParentID,
			Service:    span.Service,
			Name:       span.Name,
			Kind:       span.Kind,
			Start:      span.Start,
			End:        span.End,
			Duration:   span.Duration,
			XCost:      span.XCost,
			StatusCode: span.StatusCode,
			StatusMsg:  span.StatusMsg,
			Attributes: span.Attributes,
			Events:     span.Events,
			Links:      span.Links,
		})
	}
	return snapshotSession{
		Environment:    session.Environment,
		GrafanaURL:     session.GrafanaURL,
		BetterstackURL: session.BetterstackURL,
		Trace: snapshotTrace{
			TraceID:            trace.TraceID,
			RootSpanIDs:        append([]string(nil), trace.RootSpanIDs...),
			Spans:              spans,
			OperationName:      trace.OperationName,
			Duration:           trace.Duration,
			StartTime:          trace.StartTime,
			ServiceCount:       trace.ServiceCount,
			ErrorSpanCount:     trace.ErrorSpanCount,
			SpanCount:          trace.SpanCount,
			Environment:        trace.Environment,
			GrafanaExternalURL: trace.GrafanaExternalURL,
		},
		Logs: append([]domain.LogEntry(nil), session.Logs...),
	}
}

func fromSnapshotSession(s snapshotSession) *domain.Session {
	spansByID := map[string]*domain.Span{}
	spans := make([]*domain.Span, 0, len(s.Trace.Spans))
	for _, src := range s.Trace.Spans {
		span := &domain.Span{
			ID:         src.ID,
			ParentID:   src.ParentID,
			Service:    src.Service,
			Name:       src.Name,
			Kind:       src.Kind,
			Start:      src.Start,
			End:        src.End,
			Duration:   src.Duration,
			XCost:      src.XCost,
			StatusCode: src.StatusCode,
			StatusMsg:  src.StatusMsg,
			Attributes: src.Attributes,
			Events:     src.Events,
			Links:      src.Links,
		}
		spans = append(spans, span)
		spansByID[span.ID] = span
	}
	for _, span := range spans {
		if span.ParentID == "" {
			continue
		}
		parent := spansByID[span.ParentID]
		if parent == nil {
			continue
		}
		parent.Children = append(parent.Children, span)
	}
	for _, span := range spans {
		sort.Slice(span.Children, func(i, j int) bool {
			return span.Children[i].Start.Before(span.Children[j].Start)
		})
	}
	trace := &domain.Trace{
		TraceID:            s.Trace.TraceID,
		RootSpanIDs:        append([]string(nil), s.Trace.RootSpanIDs...),
		Spans:              spans,
		SpansByID:          spansByID,
		OperationName:      s.Trace.OperationName,
		Duration:           s.Trace.Duration,
		StartTime:          s.Trace.StartTime,
		ServiceCount:       s.Trace.ServiceCount,
		ErrorSpanCount:     s.Trace.ErrorSpanCount,
		SpanCount:          s.Trace.SpanCount,
		Environment:        s.Trace.Environment,
		GrafanaExternalURL: s.Trace.GrafanaExternalURL,
	}
	return &domain.Session{
		Trace:          trace,
		Logs:           append([]domain.LogEntry(nil), s.Logs...),
		Environment:    s.Environment,
		GrafanaURL:     s.GrafanaURL,
		BetterstackURL: s.BetterstackURL,
	}
}

func buildSnapshotServiceMap(trace *domain.Trace) snapshotServiceMap {
	total := requestTotalCostSnapshot(trace)
	nodes := snapshotNodeCosts(trace)
	edges := snapshotEdges(trace)
	external := snapshotExternal(trace)
	return snapshotServiceMap{
		TotalRequestCost: total.String(),
		Nodes:            nodes,
		Edges:            edges,
		External:         external,
	}
}

func snapshotNodeCosts(trace *domain.Trace) []snapshotServiceNode {
	if trace == nil {
		return nil
	}
	totals := map[string]time.Duration{}
	proxy := map[string]bool{}
	for _, span := range trace.Spans {
		if span == nil {
			continue
		}
		name := strings.TrimSpace(span.Service)
		if name == "" {
			continue
		}
		if isProxySpanSnapshot(span) {
			name = name + " [P]"
			proxy[name] = true
		}
		totals[name] += span.XCost
	}
	out := make([]snapshotServiceNode, 0, len(totals))
	for name, cost := range totals {
		out = append(out, snapshotServiceNode{Name: name, Proxy: proxy[name], Cost: cost.String()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Proxy != out[j].Proxy {
			return !out[i].Proxy
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func snapshotEdges(trace *domain.Trace) []snapshotServiceEdge {
	if trace == nil {
		return nil
	}
	type key struct{ from, fromPx, to, toPx string }
	counts := map[key]int{}
	for _, span := range trace.Spans {
		if span == nil || isProxySpanSnapshot(span) || strings.TrimSpace(span.Service) == "" {
			continue
		}
		targets := make([]snapshotTarget, 0, len(span.Children))
		for _, child := range span.Children {
			collectSnapshotTargets(child, "", "", 0, &targets)
		}
		for _, t := range targets {
			if t.service == "" || t.service == span.Service {
				continue
			}
			counts[key{span.Service, t.fromSidecar, t.service, t.toSidecar}]++
		}
	}
	out := make([]snapshotServiceEdge, 0, len(counts))
	for k, calls := range counts {
		out = append(out, snapshotServiceEdge{From: k.from, FromProxy: k.fromPx, To: k.to, ToProxy: k.toPx, Calls: calls})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From == out[j].From {
			if out[i].To == out[j].To {
				if out[i].FromProxy == out[j].FromProxy {
					return out[i].ToProxy < out[j].ToProxy
				}
				return out[i].FromProxy < out[j].FromProxy
			}
			return out[i].To < out[j].To
		}
		return out[i].From < out[j].From
	})
	return out
}

type snapshotTarget struct {
	service     string
	fromSidecar string
	toSidecar   string
}

func collectSnapshotTargets(span *domain.Span, fromSidecar, lastProxy string, proxyDepth int, out *[]snapshotTarget) {
	if span == nil {
		return
	}
	if !isProxySpanSnapshot(span) {
		t := snapshotTarget{service: span.Service, fromSidecar: fromSidecar}
		if proxyDepth > 1 {
			t.toSidecar = lastProxy
		}
		*out = append(*out, t)
		return
	}
	if fromSidecar == "" {
		fromSidecar = span.Service
	}
	lastProxy = span.Service
	for _, child := range span.Children {
		collectSnapshotTargets(child, fromSidecar, lastProxy, proxyDepth+1, out)
	}
}

func snapshotExternal(trace *domain.Trace) []snapshotServiceExternal {
	if trace == nil {
		return nil
	}
	type key struct{ from, fromPx, name, typ string }
	totals := map[key]struct {
		duration time.Duration
		calls    int
	}{}
	for _, span := range trace.Spans {
		if span == nil || strings.TrimSpace(span.Service) == "" || isProxySpanSnapshot(span) || !isOutboundKindSnapshot(span.Kind) {
			continue
		}
		targets := make([]snapshotTarget, 0, len(span.Children))
		for _, child := range span.Children {
			collectSnapshotTargets(child, "", "", 0, &targets)
		}
		hasRemote := false
		for _, t := range targets {
			if t.service != "" && t.service != span.Service {
				hasRemote = true
				break
			}
		}
		if hasRemote {
			continue
		}
		name, typ := externalNameAndTypeSnapshot(span)
		if name == "" {
			continue
		}
		fromProxy := ""
		for _, child := range span.Children {
			if isProxySpanSnapshot(child) {
				fromProxy = child.Service
				break
			}
		}
		k := key{from: span.Service, fromPx: fromProxy, name: name, typ: typ}
		a := totals[k]
		a.duration += span.Duration
		a.calls++
		totals[k] = a
	}
	out := make([]snapshotServiceExternal, 0, len(totals))
	for k, a := range totals {
		out = append(out, snapshotServiceExternal{From: k.from, FromProxy: k.fromPx, Name: k.name, Type: k.typ, Cost: a.duration.String(), Calls: a.calls})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From == out[j].From {
			return out[i].Name < out[j].Name
		}
		return out[i].From < out[j].From
	})
	return out
}

func isProxySpanSnapshot(span *domain.Span) bool {
	if span == nil || span.Attributes == nil {
		return false
	}
	v, ok := span.Attributes["component"].(string)
	return ok && strings.EqualFold(strings.TrimSpace(v), "proxy")
}

func isOutboundKindSnapshot(kind string) bool {
	k := strings.ToLower(strings.TrimSpace(kind))
	return strings.Contains(k, "client") || strings.Contains(k, "producer")
}

func externalNameAndTypeSnapshot(span *domain.Span) (string, string) {
	if span == nil {
		return "", "external"
	}
	attrs := span.Attributes
	lookup := func(key string) string {
		if attrs == nil {
			return ""
		}
		v, ok := attrs[key]
		if !ok {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
	for _, k := range []string{"peer.service", "server.address", "net.peer.name", "http.host"} {
		if v := lookup(k); v != "" {
			return v, "external"
		}
	}
	if system := lookup("db.system"); system != "" {
		if name := lookup("db.name"); name != "" {
			return system + "/" + name, "db"
		}
		return system, "db"
	}
	if system := lookup("messaging.system"); system != "" {
		if destination := lookup("messaging.destination.name"); destination != "" {
			return system + "/" + destination, "messaging"
		}
		if destination := lookup("messaging.destination"); destination != "" {
			return system + "/" + destination, "messaging"
		}
		return system, "messaging"
	}
	if name := strings.TrimSpace(span.Name); name != "" {
		return name, "external"
	}
	return "external", "external"
}

func requestTotalCostSnapshot(trace *domain.Trace) time.Duration {
	if trace == nil {
		return 0
	}
	type interval struct{ start, end time.Time }
	intervals := make([]interval, 0, len(trace.RootSpanIDs))
	for _, rootID := range trace.RootSpanIDs {
		span := trace.SpansByID[rootID]
		if span == nil || !span.End.After(span.Start) {
			continue
		}
		intervals = append(intervals, interval{start: span.Start, end: span.End})
	}
	if len(intervals) == 0 {
		return 0
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start.Before(intervals[j].start) })
	total := time.Duration(0)
	cur := intervals[0]
	for i := 1; i < len(intervals); i++ {
		next := intervals[i]
		if !next.start.After(cur.end) {
			if next.end.After(cur.end) {
				cur.end = next.end
			}
			continue
		}
		total += cur.end.Sub(cur.start)
		cur = next
	}
	total += cur.end.Sub(cur.start)
	return total
}
