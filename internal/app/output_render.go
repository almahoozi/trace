package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/almahoozi/trace/internal/domain"
)

type OutputFormat string

const (
	OutputFormatJSON  OutputFormat = "json"
	OutputFormatText  OutputFormat = "text"
	OutputFormatHTML  OutputFormat = "html"
	OutputFormatImage OutputFormat = "image"
)

type OutputView string

const (
	OutputViewAll        OutputView = "all"
	OutputViewTrace      OutputView = "trace"
	OutputViewServiceMap OutputView = "service-map"
	OutputViewLogs       OutputView = "logs"
)

func MarshalSessionOutputJSON(session *domain.Session, view OutputView) ([]byte, error) {
	if session == nil || session.Trace == nil {
		return nil, fmt.Errorf("missing session trace data")
	}

	full := Snapshot{
		Version:    SnapshotVersion,
		SavedAt:    time.Now().UTC(),
		Session:    toSnapshotSession(session),
		ServiceMap: buildSnapshotServiceMap(session.Trace),
	}

	var payload any
	switch view {
	case OutputViewAll:
		payload = full
	case OutputViewTrace:
		payload = map[string]any{"trace": full.Session.Trace}
	case OutputViewServiceMap:
		payload = map[string]any{"service_map": full.ServiceMap}
	case OutputViewLogs:
		payload = map[string]any{"logs": full.Session.Logs}
	default:
		return nil, fmt.Errorf("unsupported output view %q", view)
	}

	buf, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(buf, '\n'), nil
}

func RenderSessionOutputText(session *domain.Session, view OutputView) (string, error) {
	if session == nil || session.Trace == nil {
		return "", fmt.Errorf("missing session trace data")
	}

	switch view {
	case OutputViewTrace:
		return renderTraceText(session), nil
	case OutputViewServiceMap:
		buf, err := json.MarshalIndent(buildSnapshotServiceMap(session.Trace), "", "  ")
		if err != nil {
			return "", err
		}
		return string(append(buf, '\n')), nil
	case OutputViewLogs:
		return renderLogsText(session.Logs), nil
	case OutputViewAll:
		traceText := renderTraceText(session)
		logsText := renderLogsText(session.Logs)
		serviceBuf, err := json.MarshalIndent(buildSnapshotServiceMap(session.Trace), "", "  ")
		if err != nil {
			return "", err
		}
		return strings.TrimRight(traceText, "\n") + "\n\n--- SERVICE MAP ---\n" + string(serviceBuf) + "\n\n--- LOGS ---\n" + strings.TrimLeft(logsText, "\n"), nil
	default:
		return "", fmt.Errorf("unsupported output view %q", view)
	}
}

func RenderSessionOutputHTML(session *domain.Session, view OutputView) (string, error) {
	if session == nil || session.Trace == nil {
		return "", fmt.Errorf("missing session trace data")
	}

	traceText := renderTraceText(session)
	logsText := renderLogsText(session.Logs)
	serviceBuf, err := json.MarshalIndent(buildSnapshotServiceMap(session.Trace), "", "  ")
	if err != nil {
		return "", err
	}

	showTrace := view == OutputViewAll || view == OutputViewTrace
	showServiceMap := view == OutputViewAll || view == OutputViewServiceMap
	showLogs := view == OutputViewAll || view == OutputViewLogs

	var b strings.Builder
	b.WriteString("<!doctype html>\n")
	b.WriteString("<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>trace output</title>\n")
	b.WriteString("<style>body{font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;background:#0f1115;color:#e8ebf1;margin:0;padding:0}header{padding:16px 20px;border-bottom:1px solid #2b3240;background:#151a22}h1{margin:0;font-size:16px}main{padding:16px 20px}.tabs{display:flex;gap:8px;margin-bottom:12px}.tab-btn{border:1px solid #344155;background:#111826;color:#d9deea;padding:6px 10px;border-radius:8px;cursor:pointer}.tab-btn.active{background:#1c2a3f;border-color:#5f89d6;color:#fff}.panel{display:none}.panel.active{display:block}pre{white-space:pre;overflow:auto;margin:0;padding:12px;background:#111826;border:1px solid #293244;border-radius:10px}table{width:100%;border-collapse:collapse;background:#111826;border:1px solid #293244;border-radius:10px;overflow:hidden}th,td{border-bottom:1px solid #243043;padding:6px 8px;text-align:left;vertical-align:top}th{background:#18243a;position:sticky;top:0}tr:last-child td{border-bottom:none}</style>\n")
	b.WriteString("</head>\n<body>\n")
	b.WriteString("<header><h1>Trace " + html.EscapeString(session.Trace.TraceID) + "</h1></header>\n")
	b.WriteString("<main>\n")

	if view == OutputViewAll {
		b.WriteString("<div class=\"tabs\">")
		b.WriteString("<button class=\"tab-btn active\" data-tab=\"trace\">Trace</button>")
		b.WriteString("<button class=\"tab-btn\" data-tab=\"service-map\">Service Map</button>")
		b.WriteString("<button class=\"tab-btn\" data-tab=\"logs\">Logs</button>")
		b.WriteString("</div>\n")
	}

	if showTrace {
		className := "panel"
		if view != OutputViewAll {
			className = "panel active"
		}
		if view == OutputViewAll {
			className = "panel active"
		}
		b.WriteString("<section id=\"panel-trace\" class=\"" + className + "\"><pre>")
		b.WriteString(html.EscapeString(traceText))
		b.WriteString("</pre></section>\n")
	}
	if showServiceMap {
		className := "panel"
		if view != OutputViewAll {
			className = "panel active"
		}
		b.WriteString("<section id=\"panel-service-map\" class=\"" + className + "\"><pre>")
		b.WriteString(html.EscapeString(string(serviceBuf)))
		b.WriteString("</pre></section>\n")
	}
	if showLogs {
		className := "panel"
		if view != OutputViewAll {
			className = "panel active"
		}
		b.WriteString("<section id=\"panel-logs\" class=\"" + className + "\">")
		b.WriteString(renderLogsHTMLTable(session.Logs))
		if strings.TrimSpace(logsText) == "" {
			b.WriteString("<pre>(no logs)</pre>")
		}
		b.WriteString("</section>\n")
	}

	if view == OutputViewAll {
		b.WriteString("<script>const buttons=document.querySelectorAll('.tab-btn');const panels={trace:document.getElementById('panel-trace'),'service-map':document.getElementById('panel-service-map'),logs:document.getElementById('panel-logs')};buttons.forEach((btn)=>{btn.addEventListener('click',()=>{buttons.forEach((b)=>b.classList.remove('active'));btn.classList.add('active');Object.entries(panels).forEach(([key,p])=>{if(!p)return;p.classList.toggle('active',key===btn.dataset.tab);});});});</script>\n")
	}

	b.WriteString("</main>\n</body>\n</html>\n")
	return b.String(), nil
}

func RenderSessionOutputImage(session *domain.Session, view OutputView) ([]byte, error) {
	text, err := RenderSessionOutputText(session, view)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	maxLen := 0
	for _, line := range lines {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}
	if maxLen < 40 {
		maxLen = 40
	}
	width := 24 + (maxLen * 8)
	height := 24 + (len(lines) * 18)

	var b bytes.Buffer
	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"%d\" height=\"%d\" viewBox=\"0 0 %d %d\">", width, height, width, height)
	b.WriteString("<rect x=\"0\" y=\"0\" width=\"100%\" height=\"100%\" fill=\"#111826\"/>")
	b.WriteString("<g fill=\"#e8ebf1\" font-size=\"14\" font-family=\"Menlo,Monaco,Consolas,monospace\">")
	for i, line := range lines {
		escaped := html.EscapeString(line)
		if escaped == "" {
			escaped = " "
		}
		y := 20 + (i * 18)
		fmt.Fprintf(&b, "<text x=\"12\" y=\"%d\" xml:space=\"preserve\">%s</text>", y, escaped)
	}
	b.WriteString("</g></svg>\n")
	return b.Bytes(), nil
}

func renderTraceText(session *domain.Session) string {
	if session == nil || session.Trace == nil {
		return ""
	}
	trace := session.Trace
	var b strings.Builder
	fmt.Fprintf(&b, "TRACE %s\n", trace.TraceID)
	fmt.Fprintf(&b, "Environment : %s\n", dash(session.Environment))
	fmt.Fprintf(&b, "Operation   : %s\n", dash(trace.OperationName))
	fmt.Fprintf(&b, "Duration    : %s\n", trace.Duration.Round(time.Microsecond))
	fmt.Fprintf(&b, "Start       : %s\n", trace.StartTime.Format(time.RFC3339Nano))
	fmt.Fprintf(&b, "Spans       : %d (errors: %d, services: %d)\n", trace.SpanCount, trace.ErrorSpanCount, trace.ServiceCount)
	if strings.TrimSpace(session.GrafanaURL) != "" {
		fmt.Fprintf(&b, "Grafana     : %s\n", session.GrafanaURL)
	}
	if strings.TrimSpace(session.BetterstackURL) != "" {
		fmt.Fprintf(&b, "Logs URL    : %s\n", session.BetterstackURL)
	}
	b.WriteString("\nSPAN TREE\n")

	for _, root := range orderedRootSpans(trace) {
		renderSpanTree(&b, root, 0)
	}
	if len(orderedRootSpans(trace)) == 0 {
		b.WriteString("- (no spans)\n")
	}
	return b.String()
}

func orderedRootSpans(trace *domain.Trace) []*domain.Span {
	if trace == nil {
		return nil
	}
	roots := make([]*domain.Span, 0, len(trace.RootSpanIDs))
	seen := map[string]struct{}{}
	for _, id := range trace.RootSpanIDs {
		span := trace.SpansByID[id]
		if span == nil {
			continue
		}
		roots = append(roots, span)
		seen[id] = struct{}{}
	}
	for _, span := range trace.Spans {
		if span == nil || strings.TrimSpace(span.ParentID) != "" {
			continue
		}
		if _, ok := seen[span.ID]; ok {
			continue
		}
		roots = append(roots, span)
	}
	sort.Slice(roots, func(i, j int) bool {
		if roots[i].Start.Equal(roots[j].Start) {
			return roots[i].ID < roots[j].ID
		}
		return roots[i].Start.Before(roots[j].Start)
	})
	return roots
}

func renderSpanTree(b *strings.Builder, span *domain.Span, depth int) {
	if span == nil {
		return
	}
	indent := strings.Repeat("  ", depth)
	marker := " "
	if span.HasError() {
		marker = "!"
	}
	fmt.Fprintf(b, "%s- [%s] %s :: %s (%s)\n", indent, marker, dash(span.Service), dash(span.Name), span.Duration.Round(time.Microsecond))
	children := append([]*domain.Span(nil), span.Children...)
	sort.Slice(children, func(i, j int) bool {
		if children[i].Start.Equal(children[j].Start) {
			return children[i].ID < children[j].ID
		}
		return children[i].Start.Before(children[j].Start)
	})
	for _, child := range children {
		renderSpanTree(b, child, depth+1)
	}
}

func renderLogsText(entries []domain.LogEntry) string {
	if len(entries) == 0 {
		return "(no logs)\n"
	}

	var b strings.Builder
	b.WriteString("TIMESTAMP                    SERVICE              LEVEL    MESSAGE\n")
	b.WriteString("--------------------------------------------------------------------------\n")
	for _, entry := range entries {
		ts := "-"
		if !entry.Timestamp.IsZero() {
			ts = entry.Timestamp.Format(time.RFC3339Nano)
		}
		msg := strings.TrimSpace(entry.Message)
		if msg == "" {
			msg = strings.TrimSpace(entry.RawLine)
		}
		msg = strings.ReplaceAll(msg, "\n", " ")
		if len(msg) > 140 {
			msg = msg[:137] + "..."
		}
		fmt.Fprintf(&b, "%-28s %-20s %-8s %s\n", ts, trimRunes(dash(entry.Service), 20), trimRunes(dash(entry.Level), 8), msg)
	}
	return b.String()
}

func renderLogsHTMLTable(entries []domain.LogEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<table><thead><tr><th>timestamp</th><th>service</th><th>level</th><th>message</th></tr></thead><tbody>")
	for _, entry := range entries {
		ts := "-"
		if !entry.Timestamp.IsZero() {
			ts = entry.Timestamp.Format(time.RFC3339Nano)
		}
		msg := strings.TrimSpace(entry.Message)
		if msg == "" {
			msg = strings.TrimSpace(entry.RawLine)
		}
		msg = strings.ReplaceAll(msg, "\n", " ")
		b.WriteString("<tr><td>")
		b.WriteString(html.EscapeString(ts))
		b.WriteString("</td><td>")
		b.WriteString(html.EscapeString(dash(entry.Service)))
		b.WriteString("</td><td>")
		b.WriteString(html.EscapeString(dash(entry.Level)))
		b.WriteString("</td><td>")
		b.WriteString(html.EscapeString(msg))
		b.WriteString("</td></tr>")
	}
	b.WriteString("</tbody></table>")
	return b.String()
}

func trimRunes(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func dash(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "-"
	}
	return trimmed
}
