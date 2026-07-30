package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/almahoozi/trace/internal/domain"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type OutputFormat string

const (
	OutputFormatJSON  OutputFormat = "json"
	OutputFormatText  OutputFormat = "text"
	OutputFormatHTML  OutputFormat = "html"
	OutputFormatImage OutputFormat = "image"
	OutputFormatSVG   OutputFormat = "svg"
)

type OutputView string

const (
	OutputViewAll        OutputView = "all"
	OutputViewTrace      OutputView = "trace"
	OutputViewServiceMap OutputView = "service-map"
	OutputViewLogs       OutputView = "logs"
)

type outputSummary struct {
	Environment    string `json:"environment"`
	TraceID        string `json:"trace_id"`
	OperationName  string `json:"operation_name"`
	StatusCode     *int   `json:"status_code,omitempty"`
	Duration       string `json:"duration"`
	DurationMS     int64  `json:"duration_ms"`
	StartTime      string `json:"start_time"`
	EndTime        string `json:"end_time"`
	TimeWindow     string `json:"time_window"`
	ServiceCount   int    `json:"service_count"`
	ErrorSpanCount int    `json:"error_span_count"`
	SpanCount      int    `json:"span_count"`
	LogCount       int    `json:"log_count"`
	HasErrors      bool   `json:"has_errors"`
	GrafanaURL     string `json:"grafana_url,omitempty"`
	BetterstackURL string `json:"betterstack_url,omitempty"`
}

func OutputFormatExtension(format OutputFormat) string {
	switch format {
	case OutputFormatJSON:
		return "json"
	case OutputFormatText:
		return "txt"
	case OutputFormatHTML:
		return "html"
	case OutputFormatImage:
		return "png"
	case OutputFormatSVG:
		return "svg"
	default:
		return "txt"
	}
}

func MarshalSessionOutputJSON(session *domain.Session, view OutputView) ([]byte, error) {
	if session == nil || session.Trace == nil {
		return nil, fmt.Errorf("missing session trace data")
	}

	summary := buildOutputSummary(session)
	tracePayload := buildTraceJSONPayload(session)
	serviceMapPayload := buildServiceMapJSONPayload(session.Trace)
	logsPayload := buildLogsJSONPayload(session.Logs)

	base := map[string]any{
		"environment":      summary.Environment,
		"trace_id":         summary.TraceID,
		"operation_name":   summary.OperationName,
		"duration":         summary.Duration,
		"duration_ms":      summary.DurationMS,
		"start_time":       summary.StartTime,
		"end_time":         summary.EndTime,
		"time_window":      summary.TimeWindow,
		"service_count":    summary.ServiceCount,
		"error_span_count": summary.ErrorSpanCount,
		"span_count":       summary.SpanCount,
		"log_count":        summary.LogCount,
		"has_errors":       summary.HasErrors,
		"summary":          summary,
	}
	if summary.StatusCode != nil {
		base["status_code"] = *summary.StatusCode
	}
	if summary.GrafanaURL != "" {
		base["grafana_url"] = summary.GrafanaURL
	}
	if summary.BetterstackURL != "" {
		base["betterstack_url"] = summary.BetterstackURL
	}

	var payload any
	switch view {
	case OutputViewAll:
		base["trace"] = tracePayload
		base["service_map"] = serviceMapPayload
		base["logs"] = logsPayload
		payload = base
	case OutputViewTrace:
		payload = map[string]any{
			"summary": summary,
			"trace":   tracePayload,
		}
	case OutputViewServiceMap:
		payload = map[string]any{
			"summary":     summary,
			"service_map": serviceMapPayload,
		}
	case OutputViewLogs:
		payload = map[string]any{
			"summary": summary,
			"logs":    logsPayload,
		}
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
		buf, err := json.MarshalIndent(buildServiceMapJSONPayload(session.Trace), "", "  ")
		if err != nil {
			return "", err
		}
		return string(append(buf, '\n')), nil
	case OutputViewLogs:
		return renderLogsText(session.Logs), nil
	case OutputViewAll:
		traceText := renderTraceText(session)
		logsText := renderLogsText(session.Logs)
		serviceBuf, err := json.MarshalIndent(buildServiceMapJSONPayload(session.Trace), "", "  ")
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

	summary := buildOutputSummary(session)
	serviceMap := buildServiceMapJSONPayload(session.Trace)

	showTrace := view == OutputViewAll || view == OutputViewTrace
	showServiceMap := view == OutputViewAll || view == OutputViewServiceMap
	showLogs := view == OutputViewAll || view == OutputViewLogs

	var b strings.Builder
	b.WriteString("<!doctype html>\n")
	b.WriteString("<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>trace output</title>\n")
	b.WriteString("<style>")
	b.WriteString("body{font-family:ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,sans-serif;background:#0b1020;color:#e6edf7;margin:0}")
	b.WriteString("header{padding:16px 20px;border-bottom:1px solid #26314a;background:linear-gradient(180deg,#0f1830,#0b1020)}")
	b.WriteString("h1{margin:0 0 6px 0;font-size:18px}header .meta{font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12px;color:#a8b7d4}")
	b.WriteString("main{padding:16px 20px;display:grid;gap:14px}.tabs{display:flex;gap:8px;flex-wrap:wrap}.tab-btn{border:1px solid #3a4c71;background:#101a32;color:#dce6ff;padding:7px 11px;border-radius:999px;cursor:pointer}.tab-btn.active{background:#2f4d86;border-color:#7ea6ea;color:#fff}")
	b.WriteString(".panel{display:none}.panel.active{display:block}.card{background:#0f162b;border:1px solid #2a3551;border-radius:12px;padding:12px}")
	b.WriteString(".summary-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:10px}.summary-item{background:#0c1326;border:1px solid #243350;border-radius:10px;padding:9px}.summary-item .k{font-size:11px;color:#90a3c8;text-transform:uppercase;letter-spacing:.04em}.summary-item .v{font-family:ui-monospace,Menlo,Consolas,monospace;font-size:13px;margin-top:4px}")
	b.WriteString("details.span{margin:6px 0;padding:6px 8px;border:1px solid #243350;border-radius:8px;background:#0c1326}.span summary{cursor:pointer;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:13px}.span-meta{color:#9fb0d2;font-size:12px}")
	b.WriteString("table{width:100%;border-collapse:collapse;background:#0c1326;border:1px solid #243350;border-radius:10px;overflow:hidden}th,td{border-bottom:1px solid #223150;padding:7px 8px;text-align:left;vertical-align:top}th{background:#162341;position:sticky;top:0}tr:last-child td{border-bottom:none}.log-row{cursor:pointer}.detail{display:none}.detail.active{display:table-row}.detail pre{margin:0;white-space:pre-wrap;word-break:break-word;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12px}")
	b.WriteString("pre{margin:0;white-space:pre-wrap;word-break:break-word;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12px}")
	b.WriteString("</style>\n")
	b.WriteString("</head>\n<body>\n")
	b.WriteString("<header><h1>Trace " + html.EscapeString(summary.TraceID) + "</h1><div class=\"meta\">" + html.EscapeString(summary.Environment) + " | " + html.EscapeString(summary.TimeWindow) + " | spans=" + strconv.Itoa(summary.SpanCount) + " logs=" + strconv.Itoa(summary.LogCount) + "</div></header>\n")
	b.WriteString("<main>\n")

	b.WriteString("<section class=\"card\"><div class=\"summary-grid\">")
	for _, pair := range [][2]string{
		{"operation", summary.OperationName},
		{"duration", summary.Duration},
		{"time_window", summary.TimeWindow},
		{"service_count", strconv.Itoa(summary.ServiceCount)},
		{"error_span_count", strconv.Itoa(summary.ErrorSpanCount)},
		{"span_count", strconv.Itoa(summary.SpanCount)},
		{"log_count", strconv.Itoa(summary.LogCount)},
	} {
		b.WriteString("<div class=\"summary-item\"><div class=\"k\">" + html.EscapeString(pair[0]) + "</div><div class=\"v\">" + html.EscapeString(pair[1]) + "</div></div>")
	}
	if summary.StatusCode != nil {
		b.WriteString("<div class=\"summary-item\"><div class=\"k\">status_code</div><div class=\"v\">" + strconv.Itoa(*summary.StatusCode) + "</div></div>")
	}
	b.WriteString("</div></section>\n")

	if view == OutputViewAll {
		b.WriteString("<div class=\"tabs\">")
		b.WriteString("<button class=\"tab-btn active\" data-tab=\"trace\">Trace</button>")
		b.WriteString("<button class=\"tab-btn\" data-tab=\"service-map\">Service Map</button>")
		b.WriteString("<button class=\"tab-btn\" data-tab=\"logs\">Logs</button>")
		b.WriteString("</div>\n")
	}

	if showTrace {
		className := "panel"
		if view == OutputViewAll {
			className = "panel active"
		} else {
			className = "panel active"
		}
		b.WriteString("<section id=\"panel-trace\" class=\"" + className + " card\">")
		roots := orderedRootSpans(session.Trace)
		if len(roots) == 0 {
			b.WriteString("<pre>(no spans)</pre>")
		}
		for _, root := range roots {
			renderSpanTreeHTML(&b, root)
		}
		b.WriteString("</section>\n")
	}

	if showServiceMap {
		className := "panel"
		if view != OutputViewAll {
			className = "panel active"
		}
		b.WriteString("<section id=\"panel-service-map\" class=\"" + className + " card\">")
		buf, err := json.MarshalIndent(serviceMap, "", "  ")
		if err != nil {
			return "", err
		}
		b.WriteString("<pre>" + html.EscapeString(string(buf)) + "</pre>")
		b.WriteString("</section>\n")
	}

	if showLogs {
		className := "panel"
		if view != OutputViewAll {
			className = "panel active"
		}
		b.WriteString("<section id=\"panel-logs\" class=\"" + className + " card\">")
		b.WriteString(renderLogsHTMLTableInteractive(session.Logs))
		b.WriteString("</section>\n")
	}

	b.WriteString("<script>")
	b.WriteString("const tabs=document.querySelectorAll('.tab-btn');const panels={trace:document.getElementById('panel-trace'),'service-map':document.getElementById('panel-service-map'),logs:document.getElementById('panel-logs')};tabs.forEach((btn)=>{btn.addEventListener('click',()=>{tabs.forEach((t)=>t.classList.remove('active'));btn.classList.add('active');Object.entries(panels).forEach(([name,p])=>{if(!p)return;p.classList.toggle('active',name===btn.dataset.tab);});});});")
	b.WriteString("document.querySelectorAll('tr.log-row').forEach((row)=>{row.addEventListener('click',()=>{const id=row.getAttribute('data-id');const detail=document.getElementById('detail-'+id);if(!detail)return;detail.classList.toggle('active');});});")
	b.WriteString("</script>")

	b.WriteString("</main>\n</body>\n</html>\n")
	return b.String(), nil
}

func RenderSessionOutputImage(session *domain.Session, view OutputView) ([]byte, error) {
	text, err := RenderSessionOutputText(session, view)
	if err != nil {
		return nil, err
	}
	lines := normalizeOutputLines(text)
	maxLen := longestLineLen(lines)
	if maxLen < 42 {
		maxLen = 42
	}
	width := 36 + (maxLen * 7)
	height := 40 + (len(lines) * 15)

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 11, G: 16, B: 32, A: 255}}, image.Point{}, draw.Src)

	face := basicfont.Face7x13
	d := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: color.RGBA{R: 232, G: 237, B: 247, A: 255}},
		Face: face,
	}
	for i, line := range lines {
		d.Dot = fixed.P(18, 24+(i*15))
		d.DrawString(line)
	}

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func RenderSessionOutputSVG(session *domain.Session, view OutputView) ([]byte, error) {
	if session == nil || session.Trace == nil {
		return nil, fmt.Errorf("missing session trace data")
	}

	summary := buildOutputSummary(session)
	serviceMap := buildSnapshotServiceMap(session.Trace)
	showTrace := view == OutputViewAll || view == OutputViewTrace
	showServiceMap := view == OutputViewAll || view == OutputViewServiceMap
	showLogs := view == OutputViewAll || view == OutputViewLogs

	traceLines := svgTracePreviewLines(session.Trace, 26)
	logLines := svgLogPreviewLines(session.Logs, 10)

	width := 1400
	margin := 30
	y := 30
	headerH := 112
	summaryH := 120
	traceH := 0
	logsH := 0
	serviceH := 0
	if showTrace {
		traceH = maxInt(240, 90+len(traceLines)*18)
	}
	if showServiceMap {
		serviceH = 150
	}
	if showLogs {
		logsH = maxInt(220, 90+len(logLines)*18)
	}

	height := margin + headerH + 18 + summaryH
	if traceH > 0 {
		height += 20 + traceH
	}
	if serviceH > 0 {
		height += 20 + serviceH
	}
	if logsH > 0 {
		height += 20 + logsH
	}
	height += margin

	var b bytes.Buffer
	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"%d\" height=\"%d\" viewBox=\"0 0 %d %d\">", width, height, width, height)
	b.WriteString("<defs><linearGradient id=\"bg\" x1=\"0\" y1=\"0\" x2=\"1\" y2=\"1\"><stop offset=\"0%\" stop-color=\"#0b1225\"/><stop offset=\"100%\" stop-color=\"#131b34\"/></linearGradient><linearGradient id=\"header\" x1=\"0\" y1=\"0\" x2=\"1\" y2=\"0\"><stop offset=\"0%\" stop-color=\"#1e315f\"/><stop offset=\"100%\" stop-color=\"#26507c\"/></linearGradient></defs>")
	b.WriteString("<rect x=\"0\" y=\"0\" width=\"100%\" height=\"100%\" fill=\"url(#bg)\"/>")

	cardW := width - (margin * 2)

	// Header
	fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"18\" fill=\"url(#header)\" opacity=\"0.96\"/>", margin, y, cardW, headerH)
	fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" fill=\"#f8fbff\" font-family=\"Inter,Segoe UI,Arial,sans-serif\" font-size=\"30\" font-weight=\"700\">Trace %s</text>", margin+26, y+46, escapeSVG(svgTruncate(summary.TraceID, 64)))
	subtitle := summary.Environment + "  |  " + summary.OperationName
	fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" fill=\"#d5e1ff\" font-family=\"Inter,Segoe UI,Arial,sans-serif\" font-size=\"16\">%s</text>", margin+26, y+74, escapeSVG(svgTruncate(subtitle, 96)))
	statusText := "status: -"
	if summary.StatusCode != nil {
		statusText = fmt.Sprintf("status: %d", *summary.StatusCode)
	}
	fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" fill=\"#d5e1ff\" font-family=\"Menlo,Monaco,Consolas,monospace\" font-size=\"13\">%s</text>", margin+26, y+96, escapeSVG(statusText+"   duration: "+summary.Duration+"   logs: "+strconv.Itoa(summary.LogCount)))
	y += headerH + 18

	// Summary chips
	fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"14\" fill=\"#121a31\" stroke=\"#2a3655\"/>", margin, y, cardW, summaryH)
	chipY := y + 18
	chipX := margin + 18
	chipW := 156
	for _, chip := range []struct{ label, value string }{
		{label: "services", value: strconv.Itoa(summary.ServiceCount)},
		{label: "errors", value: strconv.Itoa(summary.ErrorSpanCount)},
		{label: "spans", value: strconv.Itoa(summary.SpanCount)},
		{label: "logs", value: strconv.Itoa(summary.LogCount)},
		{label: "window", value: svgTruncate(summary.TimeWindow, 34)},
	} {
		cw := chipW
		if chip.label == "window" {
			cw = 460
		}
		fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"36\" rx=\"9\" fill=\"#0e1428\" stroke=\"#263452\"/>", chipX, chipY, cw)
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" fill=\"#90a3cd\" font-family=\"Inter,Segoe UI,Arial,sans-serif\" font-size=\"11\" text-transform=\"uppercase\">%s</text>", chipX+10, chipY+14, escapeSVG(chip.label))
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" fill=\"#edf3ff\" font-family=\"Menlo,Monaco,Consolas,monospace\" font-size=\"12\">%s</text>", chipX+10, chipY+29, escapeSVG(chip.value))
		chipX += cw + 12
	}
	y += summaryH

	if showTrace {
		y += 20
		fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"14\" fill=\"#121a31\" stroke=\"#2a3655\"/>", margin, y, cardW, traceH)
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" fill=\"#e9f0ff\" font-family=\"Inter,Segoe UI,Arial,sans-serif\" font-size=\"16\" font-weight=\"700\">Trace Tree</text>", margin+18, y+28)
		lineY := y + 52
		for _, line := range traceLines {
			fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" fill=\"#c7d5f5\" font-family=\"Menlo,Monaco,Consolas,monospace\" font-size=\"13\">%s</text>", margin+18, lineY, escapeSVG(svgTruncate(line, 150)))
			lineY += 18
		}
		y += traceH
	}

	if showServiceMap {
		y += 20
		fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"14\" fill=\"#121a31\" stroke=\"#2a3655\"/>", margin, y, cardW, serviceH)
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" fill=\"#e9f0ff\" font-family=\"Inter,Segoe UI,Arial,sans-serif\" font-size=\"16\" font-weight=\"700\">Service Map</text>", margin+18, y+28)
		mapLine := fmt.Sprintf("nodes=%d   edges=%d   external=%d   total_cost=%s", len(serviceMap.Nodes), len(serviceMap.Edges), len(serviceMap.External), serviceMap.TotalRequestCost)
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" fill=\"#c7d5f5\" font-family=\"Menlo,Monaco,Consolas,monospace\" font-size=\"13\">%s</text>", margin+18, y+54, escapeSVG(svgTruncate(mapLine, 150)))

		nodeNames := make([]string, 0, minInt(8, len(serviceMap.Nodes)))
		for i, node := range serviceMap.Nodes {
			if i >= 8 {
				break
			}
			nodeNames = append(nodeNames, node.Name)
		}
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" fill=\"#9db2dd\" font-family=\"Menlo,Monaco,Consolas,monospace\" font-size=\"12\">services: %s</text>", margin+18, y+78, escapeSVG(svgTruncate(strings.Join(nodeNames, ", "), 150)))
		y += serviceH
	}

	if showLogs {
		y += 20
		fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"14\" fill=\"#121a31\" stroke=\"#2a3655\"/>", margin, y, cardW, logsH)
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" fill=\"#e9f0ff\" font-family=\"Inter,Segoe UI,Arial,sans-serif\" font-size=\"16\" font-weight=\"700\">Logs Preview</text>", margin+18, y+28)
		lineY := y + 52
		for _, line := range logLines {
			fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" fill=\"#c7d5f5\" font-family=\"Menlo,Monaco,Consolas,monospace\" font-size=\"12\">%s</text>", margin+18, lineY, escapeSVG(svgTruncate(line, 160)))
			lineY += 18
		}
		y += logsH
	}

	b.WriteString("</svg>\n")
	return b.Bytes(), nil
}

func svgTracePreviewLines(trace *domain.Trace, maxLines int) []string {
	if maxLines <= 0 {
		maxLines = 1
	}
	lines := buildTraceTreeOutline(trace)
	if len(lines) == 0 {
		return []string{"(no spans)"}
	}
	if len(lines) > maxLines {
		preview := append([]string(nil), lines[:maxLines-1]...)
		preview = append(preview, fmt.Sprintf("... +%d more spans", len(lines)-maxLines+1))
		return preview
	}
	return lines
}

func svgLogPreviewLines(entries []domain.LogEntry, maxLines int) []string {
	if maxLines <= 0 {
		maxLines = 1
	}
	if len(entries) == 0 {
		return []string{"(no logs)"}
	}
	preview := make([]string, 0, minInt(maxLines, len(entries)))
	for i, entry := range entries {
		if i >= maxLines {
			preview = append(preview, fmt.Sprintf("... +%d more logs", len(entries)-maxLines))
			break
		}
		ts := "-"
		if !entry.Timestamp.IsZero() {
			ts = entry.Timestamp.Format("15:04:05.000")
		}
		message := strings.TrimSpace(entry.Message)
		if message == "" {
			message = strings.TrimSpace(entry.RawLine)
		}
		message = strings.ReplaceAll(message, "\n", " ")
		preview = append(preview, ts+"  "+dash(entry.Service)+"  "+dash(entry.Level)+"  "+message)
	}
	return preview
}

func svgTruncate(value string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxChars {
		return value
	}
	if maxChars <= 3 {
		return string(runes[:maxChars])
	}
	return string(runes[:maxChars-3]) + "..."
}

func escapeSVG(value string) string {
	return html.EscapeString(value)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func buildOutputSummary(session *domain.Session) outputSummary {
	trace := session.Trace
	start := trace.StartTime
	end := start.Add(trace.Duration)
	statusCode := traceStatusCode(trace)
	return outputSummary{
		Environment:    dash(session.Environment),
		TraceID:        trace.TraceID,
		OperationName:  dash(trace.OperationName),
		StatusCode:     statusCode,
		Duration:       formatAdaptiveDuration(trace.Duration),
		DurationMS:     trace.Duration.Milliseconds(),
		StartTime:      start.Format(time.RFC3339Nano),
		EndTime:        end.Format(time.RFC3339Nano),
		TimeWindow:     start.Format(time.RFC3339Nano) + " / " + end.Format(time.RFC3339Nano),
		ServiceCount:   trace.ServiceCount,
		ErrorSpanCount: trace.ErrorSpanCount,
		SpanCount:      trace.SpanCount,
		LogCount:       len(session.Logs),
		HasErrors:      trace.ErrorSpanCount > 0,
		GrafanaURL:     strings.TrimSpace(session.GrafanaURL),
		BetterstackURL: strings.TrimSpace(session.BetterstackURL),
	}
}

func buildTraceJSONPayload(session *domain.Session) map[string]any {
	trace := session.Trace
	spans := make([]map[string]any, 0, len(trace.Spans))
	for _, span := range trace.Spans {
		if span == nil {
			continue
		}
		spans = append(spans, map[string]any{
			"id":          span.ID,
			"parent_id":   span.ParentID,
			"service":     span.Service,
			"name":        span.Name,
			"kind":        span.Kind,
			"start":       span.Start,
			"end":         span.End,
			"duration":    formatAdaptiveDuration(span.Duration),
			"x_cost":      formatAdaptiveDuration(span.XCost),
			"status_code": span.StatusCode,
			"status_msg":  span.StatusMsg,
			"attributes":  span.Attributes,
			"events":      span.Events,
			"links":       span.Links,
		})
	}
	return map[string]any{
		"trace_id":             trace.TraceID,
		"operation_name":       trace.OperationName,
		"root_span_ids":        append([]string(nil), trace.RootSpanIDs...),
		"start_time":           trace.StartTime,
		"end_time":             trace.StartTime.Add(trace.Duration),
		"duration":             formatAdaptiveDuration(trace.Duration),
		"service_count":        trace.ServiceCount,
		"error_span_count":     trace.ErrorSpanCount,
		"span_count":           trace.SpanCount,
		"grafana_external_url": trace.GrafanaExternalURL,
		"tree":                 buildTraceTreeOutline(trace),
		"spans":                spans,
	}
}

func buildServiceMapJSONPayload(trace *domain.Trace) map[string]any {
	snapshot := buildSnapshotServiceMap(trace)

	return map[string]any{
		"total_request_cost": snapshot.TotalRequestCost,
		"nodes":              snapshot.Nodes,
		"edges":              snapshot.Edges,
		"external":           snapshot.External,
		"map":                buildServiceMapTreeFromTrace(trace),
	}
}

func buildLogsJSONPayload(entries []domain.LogEntry) []any {
	out := make([]any, 0, len(entries))
	for _, entry := range entries {
		ts := ""
		if !entry.Timestamp.IsZero() {
			ts = entry.Timestamp.Format(time.RFC3339Nano)
		}
		msg := strings.TrimSpace(entry.Message)
		if msg == "" {
			msg = strings.TrimSpace(entry.RawLine)
		}
		out = append(out, map[string]any{
			"timestamp": ts,
			"service":   entry.Service,
			"level":     entry.Level,
			"message":   msg,
			"raw_line":  entry.RawLine,
			"json":      entry.JSON,
			"labels":    entry.Labels,
			"table": map[string]any{
				"timestamp": ts,
				"service":   entry.Service,
				"level":     entry.Level,
				"message":   msg,
			},
		})
	}
	return out
}

func buildServiceMapTreeFromTrace(trace *domain.Trace) map[string]any {
	if trace == nil {
		return map[string]any{}
	}
	services := serviceListForMap(trace)
	if len(services) == 0 {
		return map[string]any{}
	}
	totalCost := requestTotalCostSnapshot(trace)
	nodeCosts := serviceNodeCostsForMap(trace)
	edges := buildServiceEdgesForMap(trace)
	externals := buildExternalDependenciesForMap(trace)

	edgesByFrom := map[string][]serviceEdgeLite{}
	incoming := map[string]int{}
	for _, edge := range edges {
		edgesByFrom[edge.from] = append(edgesByFrom[edge.from], edge)
		incoming[edge.to]++
	}
	externalsByFrom := map[string][]externalDependencyLite{}
	for _, dep := range externals {
		externalsByFrom[dep.from] = append(externalsByFrom[dep.from], dep)
	}

	roots := make([]string, 0)
	for _, service := range services {
		if incoming[service] == 0 {
			roots = append(roots, service)
		}
	}
	if len(roots) == 0 {
		roots = append(roots, services...)
	}
	sort.Strings(roots)

	mapTree := map[string]any{}
	for _, root := range roots {
		seen := map[string]bool{root: true}
		leaf := len(edgesByFrom[root]) == 0 && len(externalsByFrom[root]) == 0
		key := renderServiceMapServiceLabel(root, primaryProxyForServiceFromEdges(root, edgesByFrom), nodeCosts[root], totalCost, 0, leaf)
		children := buildServiceMapSubtree(root, edgesByFrom, externalsByFrom, nodeCosts, totalCost, seen)
		if len(children) == 0 {
			mapTree[key] = ""
		} else {
			mapTree[key] = children
		}
	}
	return mapTree
}

func buildServiceMapSubtree(
	service string,
	edgesByFrom map[string][]serviceEdgeLite,
	externalsByFrom map[string][]externalDependencyLite,
	nodeCosts map[string]time.Duration,
	totalCost time.Duration,
	seen map[string]bool,
) map[string]any {
	out := map[string]any{}

	for _, edge := range edgesByFrom[service] {
		leaf := len(edgesByFrom[edge.to]) == 0 && len(externalsByFrom[edge.to]) == 0
		callsInLabel := edge.count
		if leaf {
			callsInLabel = 0
		}
		childKey := renderServiceMapServiceLabel(edge.to, edge.toProxy, nodeCosts[edge.to], totalCost, callsInLabel, leaf)
		if seen[edge.to] {
			out[childKey] = "(cycle)"
			continue
		}
		nextSeen := make(map[string]bool, len(seen)+1)
		for k, v := range seen {
			nextSeen[k] = v
		}
		nextSeen[edge.to] = true
		subtree := buildServiceMapSubtree(edge.to, edgesByFrom, externalsByFrom, nodeCosts, totalCost, nextSeen)
		if len(subtree) == 0 {
			if edge.count > 0 {
				out[childKey] = "x" + strconv.Itoa(edge.count)
			} else {
				out[childKey] = ""
			}
		} else {
			out[childKey] = subtree
		}
	}

	for _, dep := range externalsByFrom[service] {
		leafKey := renderServiceMapExternalLabel(dep.name, dep.fromProxy, dep.duration, totalCost, true)
		out[leafKey] = "x" + strconv.Itoa(dep.count)
	}

	return out
}

type serviceEdgeLite struct {
	from      string
	fromProxy string
	to        string
	toProxy   string
	count     int
}

type edgeTargetLite struct {
	service     string
	fromSidecar string
	toSidecar   string
}

type externalDependencyLite struct {
	from      string
	fromProxy string
	name      string
	duration  time.Duration
	count     int
}

func renderServiceMapServiceLabel(service, proxy string, cost, totalCost time.Duration, calls int, includePercent bool) string {
	label := renderServiceMapEntityLabel(service, proxy)
	label += " [" + formatCostDisplayForMap(cost, totalCost, includePercent) + "]"
	if calls > 0 {
		label += " x" + strconv.Itoa(calls)
	}
	return label
}

func renderServiceMapExternalLabel(name, proxy string, cost, totalCost time.Duration, includePercent bool) string {
	label := renderServiceMapEntityLabel(name, proxy)
	label += " [" + formatCostDisplayForMap(cost, totalCost, includePercent) + "]"
	return label
}

func renderServiceMapEntityLabel(name, proxy string) string {
	base := strings.TrimSpace(name)
	if base == "" {
		base = "-"
	}
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return base
	}
	proxy = strings.TrimSuffix(proxy, " [P]")
	return base + " (" + proxy + " [P])"
}

func formatCostDisplayForMap(cost, total time.Duration, includePercent bool) string {
	base := formatDurationDisplayForMap(cost)
	if !includePercent || total <= 0 {
		return base
	}
	pct := (float64(cost) / float64(total)) * 100
	return fmt.Sprintf("%s (%.1f%%)", base, pct)
}

func formatDurationDisplayForMap(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d == 0 {
		return "0s"
	}
	return d.Round(time.Microsecond).String()
}

func primaryProxyForServiceFromEdges(service string, edgesByFrom map[string][]serviceEdgeLite) string {
	counts := map[string]int{}
	for _, edge := range edgesByFrom[service] {
		if edge.from != service {
			continue
		}
		if strings.TrimSpace(edge.fromProxy) == "" {
			continue
		}
		counts[edge.fromProxy] += edge.count
	}
	best := ""
	bestCount := 0
	for proxy, calls := range counts {
		if calls > bestCount {
			best = proxy
			bestCount = calls
		}
	}
	return best
}

func serviceListForMap(trace *domain.Trace) []string {
	set := map[string]struct{}{}
	for _, span := range trace.Spans {
		if span == nil {
			continue
		}
		service := strings.TrimSpace(span.Service)
		if service == "" || isProxySpanSnapshot(span) {
			continue
		}
		set[service] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for service := range set {
		out = append(out, service)
	}
	sort.Strings(out)
	return out
}

func serviceNodeCostsForMap(trace *domain.Trace) map[string]time.Duration {
	out := map[string]time.Duration{}
	for _, span := range trace.Spans {
		if span == nil {
			continue
		}
		service := strings.TrimSpace(span.Service)
		if service == "" || isProxySpanSnapshot(span) {
			continue
		}
		out[service] += span.XCost
	}
	return out
}

func buildServiceEdgesForMap(trace *domain.Trace) []serviceEdgeLite {
	counts := map[string]int{}
	for _, span := range trace.Spans {
		if span == nil || strings.TrimSpace(span.Service) == "" || isProxySpanSnapshot(span) {
			continue
		}
		targets := make([]edgeTargetLite, 0, len(span.Children))
		for _, child := range span.Children {
			collectProxyRoutedTargetsForMap(child, "", "", 0, &targets)
		}
		for _, target := range targets {
			if target.service == "" || target.service == span.Service {
				continue
			}
			key := strings.Join([]string{span.Service, target.fromSidecar, target.service, target.toSidecar}, "\x00")
			counts[key]++
		}
	}
	out := make([]serviceEdgeLite, 0, len(counts))
	for key, count := range counts {
		parts := strings.Split(key, "\x00")
		out = append(out, serviceEdgeLite{from: parts[0], fromProxy: parts[1], to: parts[2], toProxy: parts[3], count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].from == out[j].from {
			if out[i].to == out[j].to {
				if out[i].fromProxy == out[j].fromProxy {
					return out[i].toProxy < out[j].toProxy
				}
				return out[i].fromProxy < out[j].fromProxy
			}
			return out[i].to < out[j].to
		}
		return out[i].from < out[j].from
	})
	return out
}

func collectProxyRoutedTargetsForMap(span *domain.Span, fromSidecar, lastProxy string, proxyDepth int, out *[]edgeTargetLite) {
	if span == nil {
		return
	}
	if !isProxySpanSnapshot(span) {
		target := edgeTargetLite{service: span.Service, fromSidecar: fromSidecar}
		if proxyDepth > 1 {
			target.toSidecar = lastProxy
		}
		*out = append(*out, target)
		return
	}
	if fromSidecar == "" {
		fromSidecar = span.Service
	}
	lastProxy = span.Service
	for _, child := range span.Children {
		collectProxyRoutedTargetsForMap(child, fromSidecar, lastProxy, proxyDepth+1, out)
	}
}

func buildExternalDependenciesForMap(trace *domain.Trace) []externalDependencyLite {
	type depStats struct {
		duration time.Duration
		count    int
	}
	stats := map[string]depStats{}
	for _, span := range trace.Spans {
		if span == nil || strings.TrimSpace(span.Service) == "" || isProxySpanSnapshot(span) || !isOutboundKindSnapshot(span.Kind) {
			continue
		}
		targets := make([]edgeTargetLite, 0, len(span.Children))
		for _, child := range span.Children {
			collectProxyRoutedTargetsForMap(child, "", "", 0, &targets)
		}
		hasInstrumentedRemote := false
		for _, target := range targets {
			if target.service != "" && target.service != span.Service {
				hasInstrumentedRemote = true
				break
			}
		}
		if hasInstrumentedRemote {
			continue
		}
		depName, _ := externalNameAndTypeSnapshot(span)
		if strings.TrimSpace(depName) == "" {
			continue
		}
		sidecar := ""
		for _, child := range span.Children {
			if isProxySpanSnapshot(child) {
				sidecar = strings.TrimSpace(child.Service)
				break
			}
		}
		key := strings.Join([]string{span.Service, sidecar, depName}, "\x00")
		agg := stats[key]
		agg.count++
		agg.duration += span.Duration
		stats[key] = agg
	}
	out := make([]externalDependencyLite, 0, len(stats))
	for key, agg := range stats {
		parts := strings.Split(key, "\x00")
		out = append(out, externalDependencyLite{from: parts[0], fromProxy: parts[1], name: parts[2], duration: agg.duration, count: agg.count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].from == out[j].from {
			if out[i].name == out[j].name {
				return out[i].fromProxy < out[j].fromProxy
			}
			return out[i].name < out[j].name
		}
		return out[i].from < out[j].from
	})
	return out
}

func formatAdaptiveDuration(d time.Duration) string {
	sign := ""
	if d < 0 {
		sign = "-"
		d = -d
	}
	if d < time.Millisecond {
		return sign + formatFloatTrim(float64(d)/float64(time.Microsecond), 1) + "us"
	}
	if d < 10*time.Second {
		return sign + formatFloatTrim(float64(d)/float64(time.Millisecond), 2) + "ms"
	}
	return sign + formatFloatTrim(float64(d)/float64(time.Second), 3) + "s"
}

func formatFloatTrim(value float64, decimals int) string {
	text := strconv.FormatFloat(value, 'f', decimals, 64)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	if text == "" {
		return "0"
	}
	return text
}

func traceStatusCode(trace *domain.Trace) *int {
	root := firstRootSpan(trace)
	if root == nil || root.Attributes == nil {
		return nil
	}
	raw, ok := root.Attributes["http.response.status_code"]
	if !ok {
		return nil
	}
	status, ok := parseHTTPStatusCode(raw)
	if !ok {
		return nil
	}
	return &status
}

func buildTraceTreeOutline(trace *domain.Trace) []string {
	if trace == nil {
		return nil
	}
	lines := []string{}
	for _, root := range orderedRootSpans(trace) {
		appendTraceTreeOutline(&lines, root, 0)
	}
	return lines
}

func appendTraceTreeOutline(lines *[]string, span *domain.Span, depth int) {
	if span == nil {
		return
	}
	indent := strings.Repeat("  ", depth)
	marker := " "
	if span.HasError() {
		marker = "!"
	}
	*lines = append(*lines, fmt.Sprintf("%s- [%s] %s :: %s (%s)", indent, marker, dash(span.Service), dash(span.Name), span.Duration.Round(time.Microsecond)))
	children := append([]*domain.Span(nil), span.Children...)
	sort.Slice(children, func(i, j int) bool {
		if children[i].Start.Equal(children[j].Start) {
			return children[i].ID < children[j].ID
		}
		return children[i].Start.Before(children[j].Start)
	})
	for _, child := range children {
		appendTraceTreeOutline(lines, child, depth+1)
	}
}

func renderTraceText(session *domain.Session) string {
	if session == nil || session.Trace == nil {
		return ""
	}
	summary := buildOutputSummary(session)
	trace := session.Trace
	var b strings.Builder
	fmt.Fprintf(&b, "TRACE %s\n", summary.TraceID)
	fmt.Fprintf(&b, "Environment : %s\n", summary.Environment)
	fmt.Fprintf(&b, "Operation   : %s\n", summary.OperationName)
	if summary.StatusCode != nil {
		fmt.Fprintf(&b, "Status code : %d\n", *summary.StatusCode)
	}
	fmt.Fprintf(&b, "Duration    : %s\n", summary.Duration)
	fmt.Fprintf(&b, "Time window : %s\n", summary.TimeWindow)
	fmt.Fprintf(&b, "Counts      : services=%d errors=%d spans=%d logs=%d\n", summary.ServiceCount, summary.ErrorSpanCount, summary.SpanCount, summary.LogCount)
	if strings.TrimSpace(summary.GrafanaURL) != "" {
		fmt.Fprintf(&b, "Grafana     : %s\n", summary.GrafanaURL)
	}
	if strings.TrimSpace(summary.BetterstackURL) != "" {
		fmt.Fprintf(&b, "Logs URL    : %s\n", summary.BetterstackURL)
	}
	b.WriteString("\nSPAN TREE\n")

	roots := orderedRootSpans(trace)
	for _, root := range roots {
		renderSpanTree(&b, root, 0)
	}
	if len(roots) == 0 {
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

func renderSpanTreeHTML(b *strings.Builder, span *domain.Span) {
	if span == nil {
		return
	}
	hasChildren := len(span.Children) > 0
	if !hasChildren {
		b.WriteString("<details class=\"span\" open><summary>")
		b.WriteString(html.EscapeString(dash(span.Service) + " :: " + dash(span.Name)))
		b.WriteString("</summary><div class=\"span-meta\">duration=" + html.EscapeString(span.Duration.Round(time.Microsecond).String()) + " status=" + html.EscapeString(dash(span.StatusCode)) + "</div></details>")
		return
	}
	b.WriteString("<details class=\"span\" open><summary>")
	b.WriteString(html.EscapeString(dash(span.Service) + " :: " + dash(span.Name)))
	b.WriteString("</summary><div class=\"span-meta\">duration=" + html.EscapeString(span.Duration.Round(time.Microsecond).String()) + " status=" + html.EscapeString(dash(span.StatusCode)) + "</div>")
	children := append([]*domain.Span(nil), span.Children...)
	sort.Slice(children, func(i, j int) bool {
		if children[i].Start.Equal(children[j].Start) {
			return children[i].ID < children[j].ID
		}
		return children[i].Start.Before(children[j].Start)
	})
	for _, child := range children {
		renderSpanTreeHTML(b, child)
	}
	b.WriteString("</details>")
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

func renderLogsHTMLTableInteractive(entries []domain.LogEntry) string {
	if len(entries) == 0 {
		return "<pre>(no logs)</pre>"
	}
	var b strings.Builder
	b.WriteString("<table><thead><tr><th>timestamp</th><th>service</th><th>level</th><th>message</th></tr></thead><tbody>")
	for i, entry := range entries {
		ts := "-"
		if !entry.Timestamp.IsZero() {
			ts = entry.Timestamp.Format(time.RFC3339Nano)
		}
		msg := strings.TrimSpace(entry.Message)
		if msg == "" {
			msg = strings.TrimSpace(entry.RawLine)
		}
		if len(msg) > 180 {
			msg = msg[:177] + "..."
		}
		b.WriteString("<tr class=\"log-row\" data-id=\"" + strconv.Itoa(i) + "\"><td>")
		b.WriteString(html.EscapeString(ts))
		b.WriteString("</td><td>")
		b.WriteString(html.EscapeString(dash(entry.Service)))
		b.WriteString("</td><td>")
		b.WriteString(html.EscapeString(dash(entry.Level)))
		b.WriteString("</td><td>")
		b.WriteString(html.EscapeString(msg))
		b.WriteString("</td></tr>")

		detail := map[string]any{
			"timestamp": ts,
			"service":   entry.Service,
			"level":     entry.Level,
			"message":   entry.Message,
			"raw_line":  entry.RawLine,
			"json":      entry.JSON,
			"labels":    entry.Labels,
		}
		detailBuf, _ := json.MarshalIndent(detail, "", "  ")
		b.WriteString("<tr class=\"detail\" id=\"detail-" + strconv.Itoa(i) + "\"><td colspan=\"4\"><pre>")
		b.WriteString(html.EscapeString(string(detailBuf)))
		b.WriteString("</pre></td></tr>")
	}
	b.WriteString("</tbody></table>")
	return b.String()
}

func normalizeOutputLines(text string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func longestLineLen(lines []string) int {
	maxLen := 0
	for _, line := range lines {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}
	return maxLen
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
