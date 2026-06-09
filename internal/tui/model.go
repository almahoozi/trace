package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/almahoozi/trace/internal/config"
	"github.com/almahoozi/trace/internal/domain"
)

type panel int

const (
	panelTrace panel = iota
	panelServiceMap
	panelLogs
)

type traceLine struct {
	SpanID    string
	Depth     int
	Label     string
	Kind      string
	Service   string
	HasKids   bool
	Expanded  bool
	Error     bool
	Duration  time.Duration
	Start     time.Time
	End       time.Time
	LinkCount int
}

type logColumn struct {
	Header string
	Field  string
	Weight int
}

type Model struct {
	cfg     config.Config
	session *domain.Session
	openURL func(string) error

	width  int
	height int

	activePanel panel
	showHelp    bool
	jsonTree    *JSONTree
	valueView   *valueView
	fullscreen  bool
	collapsed   map[panel]bool

	traceLines       []traceLine
	traceCursor      int
	expanded         map[string]bool
	serviceMapCursor int

	allLogs          []domain.LogEntry
	filteredLogs     []domain.LogEntry
	logCursor        int
	levelThresholdIx int

	status string
}

type valueView struct {
	title  string
	lines  []string
	offset int
}

func NewModel(cfg config.Config, session *domain.Session, openURL func(string) error) Model {
	m := Model{
		cfg:          cfg,
		session:      session,
		openURL:      openURL,
		expanded:     map[string]bool{},
		collapsed:    map[panel]bool{},
		fullscreen:   cfg.UI.DefaultFullscreen,
		allLogs:      session.Logs,
		filteredLogs: session.Logs,
		status:       fmt.Sprintf("env=%s spans=%d logs=%d", session.Environment, len(session.Trace.Spans), len(session.Logs)),
	}
	m.activePanel = parseFocusPanel(cfg.UI.FocusSection)

	for _, span := range session.Trace.Spans {
		m.expanded[span.ID] = true
	}
	for _, raw := range cfg.UI.CollapsedSections {
		name, _ := parseSectionSpec(raw)
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "trace", "traces":
			m.collapsed[panelTrace] = true
		case "service_map", "service-map", "servicemap", "map":
			m.collapsed[panelServiceMap] = true
		case "logs", "log":
			m.collapsed[panelLogs] = true
		}
	}
	m.traceLines = flattenTrace(session.Trace, m.expanded)
	m.levelThresholdIx = m.levelIndex(cfg.Logs.LevelThreshold)
	m.applyLogThreshold()
	return m
}

func parseFocusPanel(raw string) panel {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "trace", "traces":
		return panelTrace
	case "service_map", "service-map", "servicemap", "map":
		return panelServiceMap
	case "logs", "log":
		return panelLogs
	default:
		return panelTrace
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		key := msg.String()

		if m.isAction("global", "quit", key) {
			return m, tea.Quit
		}

		if m.showHelp {
			if m.isAction("global", "help", key) || m.isAction("global", "back", key) || strings.EqualFold(key, "esc") {
				m.showHelp = false
			}
			return m, nil
		}

		if m.isAction("global", "help", key) {
			m.showHelp = true
			return m, nil
		}

		if m.jsonTree != nil {
			return m.updateJSON(key)
		}

		if m.isAction("global", "switch_tab", key) {
			m.activePanel = m.nextPanel(1)
			return m, nil
		}
		if m.isAction("global", "switch_tab_back", key) {
			m.activePanel = m.nextPanel(-1)
			return m, nil
		}
		if m.isAction("global", "toggle_fullscreen", key) {
			m.fullscreen = !m.fullscreen
			return m, tea.WindowSize()
		}
		if m.isAction("global", "toggle_collapse", key) {
			m.collapsed[m.activePanel] = !m.collapsed[m.activePanel]
			return m, tea.WindowSize()
		}

		switch m.activePanel {
		case panelTrace:
			return m.updateTrace(key)
		case panelServiceMap:
			return m.updateServiceMap(key)
		default:
			return m.updateLogs(key)
		}
	}

	return m, nil
}

func (m Model) updateJSON(key string) (tea.Model, tea.Cmd) {
	if m.valueView != nil {
		return m.updateValueView(key)
	}
	if m.isAction("json", "back", key) || m.isAction("global", "back", key) {
		m.jsonTree = nil
		return m, nil
	}
	if m.isAction("json", "up", key) {
		m.jsonTree.MoveUp()
	}
	if m.isAction("json", "down", key) {
		m.jsonTree.MoveDown()
	}
	if m.isAction("json", "expand", key) {
		m.jsonTree.Expand()
	}
	if m.isAction("json", "collapse", key) {
		m.jsonTree.Collapse()
	}
	if m.isAction("json", "toggle", key) || strings.EqualFold(key, "enter") {
		if label, value, ok := m.jsonTree.CurrentScalar(); ok {
			title := "Field value"
			if label != "" {
				title = "Field value: " + label
			}
			m.valueView = newValueView(title, value, max(20, m.width-4))
			return m, nil
		}
		m.jsonTree.Toggle()
	}
	return m, nil
}

func (m Model) updateValueView(key string) (tea.Model, tea.Cmd) {
	if m.isAction("json", "back", key) || m.isAction("global", "back", key) || strings.EqualFold(key, "enter") {
		m.valueView = nil
		return m, nil
	}
	if m.isAction("json", "up", key) && m.valueView.offset > 0 {
		m.valueView.offset--
	}
	if m.isAction("json", "down", key) && m.valueView.offset < len(m.valueView.lines)-1 {
		m.valueView.offset++
	}
	return m, nil
}

func (m Model) updateTrace(key string) (tea.Model, tea.Cmd) {
	if m.isAction("trace", "up", key) && m.traceCursor > 0 {
		m.traceCursor--
	}
	if m.isAction("trace", "down", key) && m.traceCursor < len(m.traceLines)-1 {
		m.traceCursor++
	}
	if m.isAction("trace", "expand", key) {
		m.toggleTraceNode(true)
	}
	if m.isAction("trace", "collapse", key) {
		m.toggleTraceNode(false)
	}
	if m.isAction("trace", "toggle", key) || key == " " {
		m.toggleNearestSpan()
	}
	if m.isAction("trace", "details", key) {
		if span := m.currentSpan(); span != nil {
			m.jsonTree = NewJSONTreeExpandedAll(
				fmt.Sprintf("span %s (%s)", span.Name, shortID(span.ID)),
				m.traceDetailRoot(span),
			)
		}
	}
	if m.isAction("trace", "open_external", key) {
		if m.session.GrafanaURL != "" {
			if err := m.openURL(m.session.GrafanaURL); err != nil {
				m.status = "open grafana failed: " + err.Error()
			} else {
				m.status = "opened grafana trace"
			}
		}
	}
	return m, nil
}

func (m Model) updateServiceMap(_ string) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m Model) updateLogs(key string) (tea.Model, tea.Cmd) {
	if m.isAction("logs", "up", key) && m.logCursor > 0 {
		m.logCursor--
	}
	if m.isAction("logs", "down", key) && m.logCursor < len(m.filteredLogs)-1 {
		m.logCursor++
	}
	if m.isAction("logs", "level_up", key) && m.levelThresholdIx < len(m.cfg.Logs.LevelOrder)-1 {
		m.levelThresholdIx++
		m.applyLogThreshold()
	}
	if m.isAction("logs", "level_down", key) && m.levelThresholdIx > 0 {
		m.levelThresholdIx--
		m.applyLogThreshold()
	}
	if m.isAction("logs", "details", key) {
		if entry, ok := m.currentLog(); ok {
			m.jsonTree = NewJSONTreeExpandedAll("log line json", m.logDetailRoot(entry))
		}
	}
	if m.isAction("logs", "open_external", key) {
		if m.session.BetterstackURL != "" {
			if err := m.openURL(m.session.BetterstackURL); err != nil {
				m.status = "open betterstack failed: " + err.Error()
			} else {
				m.status = "opened betterstack logs"
			}
		}
	}
	return m, nil
}

func (m *Model) toggleTraceNode(expand bool) {
	line := m.currentTraceLine()
	if line == nil || !line.HasKids {
		return
	}
	m.expanded[line.SpanID] = expand
	m.traceLines = flattenTrace(m.session.Trace, m.expanded)
	if m.traceCursor >= len(m.traceLines) {
		m.traceCursor = len(m.traceLines) - 1
	}
}

func (m *Model) toggleNearestSpan() {
	line := m.currentTraceLine()
	if line == nil {
		return
	}
	if line.HasKids {
		m.expanded[line.SpanID] = !m.expanded[line.SpanID]
		m.traceLines = flattenTrace(m.session.Trace, m.expanded)
		if m.traceCursor >= len(m.traceLines) {
			m.traceCursor = max(0, len(m.traceLines)-1)
		}
		return
	}

	parentIdx := m.nearestParentWithChildrenIndex(m.traceCursor)
	if parentIdx < 0 {
		return
	}
	parentID := m.traceLines[parentIdx].SpanID
	m.expanded[parentID] = false
	m.traceLines = flattenTrace(m.session.Trace, m.expanded)
	for i := range m.traceLines {
		if m.traceLines[i].SpanID == parentID {
			m.traceCursor = i
			return
		}
	}
	m.traceCursor = max(0, min(parentIdx, len(m.traceLines)-1))
}

func (m Model) nearestParentWithChildrenIndex(from int) int {
	if from < 0 || from >= len(m.traceLines) {
		return -1
	}
	depth := m.traceLines[from].Depth
	for i := from - 1; i >= 0; i-- {
		candidate := m.traceLines[i]
		if candidate.Depth < depth && candidate.HasKids {
			return i
		}
	}
	return -1
}

func (m *Model) applyLogThreshold() {
	if len(m.cfg.Logs.LevelOrder) == 0 {
		m.filteredLogs = m.allLogs
		return
	}
	threshold := strings.ToLower(m.cfg.Logs.LevelOrder[m.levelThresholdIx])
	ix := map[string]int{}
	for i, l := range m.cfg.Logs.LevelOrder {
		ix[strings.ToLower(l)] = i
	}
	filtered := make([]domain.LogEntry, 0, len(m.allLogs))
	for _, entry := range m.allLogs {
		cur, ok := ix[strings.ToLower(entry.Level)]
		if !ok || cur >= ix[threshold] {
			filtered = append(filtered, entry)
		}
	}
	m.filteredLogs = filtered
	if m.logCursor >= len(m.filteredLogs) {
		m.logCursor = max(0, len(m.filteredLogs)-1)
	}
	m.status = fmt.Sprintf("log filter level >= %s (%d lines)", threshold, len(filtered))
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading..."
	}

	if m.showHelp {
		return clampToHeight(m.helpView(), m.height)
	}
	if m.valueView != nil {
		return clampToHeight(m.layout(m.valueViewView()), m.height)
	}
	if m.jsonTree != nil {
		return clampToHeight(m.layout(m.jsonTree.View(m.height-3)), m.height)
	}

	header := fmt.Sprintf("trace=%s env=%s operation=%s duration=%s spans=%d services=%d errors=%d",
		m.session.Trace.TraceID,
		m.session.Environment,
		m.session.Trace.OperationName,
		m.session.Trace.Duration.Round(time.Millisecond),
		m.session.Trace.SpanCount,
		m.session.Trace.ServiceCount,
		m.session.Trace.ErrorSpanCount,
	)
	headerRendered := titleStyle.Width(max(1, m.width)).Render(header)
	headerHeight := max(1, lipgloss.Height(headerRendered))

	if m.fullscreen {
		innerHeight := max(1, m.height-headerHeight-2)
		body := sectionStyle(true, m.width).Render(m.panelView(m.activePanel, innerHeight))
		return clampToHeight(lipgloss.JoinVertical(lipgloss.Left, headerRendered, body), m.height)
	}

	footer := mutedStyle.Render(m.status + " | f fullscreen | c collapse | tab/shift+tab switch | ? help")
	footerHeight := max(1, lipgloss.Height(footer))
	sectionCount := 3
	borderOverhead := 2 * sectionCount
	availableInner := max(3, m.height-headerHeight-footerHeight-borderOverhead)

	order := m.sectionOrder()
	weights := m.sectionWeights()

	innerHeights := map[panel]int{}
	for _, p := range order {
		if m.collapsed[p] {
			innerHeights[p] = 1
		}
	}

	fixed := innerHeights[panelTrace] + innerHeights[panelServiceMap] + innerHeights[panelLogs]
	remaining := max(0, availableInner-fixed)

	visible := make([]panel, 0, len(order))
	for _, p := range order {
		if !m.collapsed[p] {
			visible = append(visible, p)
		}
	}
	minPerSection := 3
	for _, p := range visible {
		if remaining <= 0 {
			break
		}
		grant := min(minPerSection, remaining)
		innerHeights[p] = grant
		remaining -= grant
	}
	weightTotal := 0
	for _, p := range visible {
		w := weights[p]
		if w <= 0 {
			w = 1
		}
		weightTotal += w
	}
	if weightTotal == 0 {
		weightTotal = 1
	}
	if remaining > 0 {
		used := 0
		for _, p := range visible {
			w := weights[p]
			if w <= 0 {
				w = 1
			}
			add := (remaining * w) / weightTotal
			innerHeights[p] += add
			used += add
		}
		left := remaining - used
		for i := 0; i < left; i++ {
			innerHeights[visible[i%len(visible)]]++
		}
	}

	var rendered []string
	for _, p := range order {
		if m.collapsed[p] {
			rendered = append(rendered, sectionStyle(m.activePanel == p, m.width).Render(panelTitle(p)+" (collapsed)"))
			continue
		}
		rendered = append(rendered, sectionStyle(m.activePanel == p, m.width).Render(m.panelView(p, max(1, innerHeights[p]))))
	}

	return clampToHeight(lipgloss.JoinVertical(lipgloss.Left, append([]string{headerRendered}, append(rendered, footer)...)...), m.height)
}

func (m Model) panelView(p panel, height int) string {
	switch p {
	case panelTrace:
		return m.traceView(height)
	case panelServiceMap:
		return m.serviceMapView(height)
	default:
		return m.logsView(height)
	}
}

func panelTitle(p panel) string {
	switch p {
	case panelTrace:
		return "Trace"
	case panelServiceMap:
		return "Service map"
	default:
		return "Logs"
	}
}

func (m Model) sectionOrder() []panel {
	defaultOrder := []panel{panelTrace, panelServiceMap, panelLogs}
	if len(m.cfg.UI.SectionOrder) == 0 {
		return defaultOrder
	}
	seen := map[panel]bool{}
	ordered := make([]panel, 0, len(defaultOrder))
	for _, raw := range m.cfg.UI.SectionOrder {
		name, _ := parseSectionSpec(raw)
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "trace", "traces":
			if !seen[panelTrace] {
				ordered = append(ordered, panelTrace)
				seen[panelTrace] = true
			}
		case "service_map", "service-map", "servicemap", "map":
			if !seen[panelServiceMap] {
				ordered = append(ordered, panelServiceMap)
				seen[panelServiceMap] = true
			}
		case "logs", "log":
			if !seen[panelLogs] {
				ordered = append(ordered, panelLogs)
				seen[panelLogs] = true
			}
		}
	}
	for _, p := range defaultOrder {
		if !seen[p] {
			ordered = append(ordered, p)
		}
	}
	return ordered
}

func (m Model) sectionWeights() map[panel]int {
	weights := map[panel]int{panelTrace: 1, panelServiceMap: 1, panelLogs: 1}
	for _, raw := range m.cfg.UI.SectionOrder {
		name, weight := parseSectionSpec(raw)
		if weight <= 0 {
			weight = 1
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "trace", "traces":
			weights[panelTrace] = weight
		case "service_map", "service-map", "servicemap", "map":
			weights[panelServiceMap] = weight
		case "logs", "log":
			weights[panelLogs] = weight
		}
	}
	return weights
}

func parseSectionSpec(raw string) (string, int) {
	item := strings.TrimSpace(raw)
	if item == "" {
		return "", 1
	}
	if idx := strings.LastIndex(item, "|"); idx > 0 && idx < len(item)-1 {
		name := strings.TrimSpace(item[:idx])
		weightRaw := strings.TrimSpace(item[idx+1:])
		if w, err := strconv.Atoi(weightRaw); err == nil && w > 0 {
			return name, w
		}
		return name, 1
	}
	return item, 1
}

func (m Model) nextPanel(step int) panel {
	order := m.sectionOrder()
	if len(order) == 0 {
		return m.activePanel
	}
	idx := 0
	for i, p := range order {
		if p == m.activePanel {
			idx = i
			break
		}
	}
	next := (idx + step) % len(order)
	if next < 0 {
		next += len(order)
	}
	return order[next]
}

func (m Model) layout(body string) string {
	return lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render("trace viewer"), body, mutedStyle.Render(m.status+" | ? help | esc back"))
}

func (m Model) traceView(height int) string {
	if len(m.traceLines) == 0 {
		return "Trace\n(no spans)"
	}
	if height < 4 {
		height = 4
	}

	var b strings.Builder
	b.WriteString("Trace timeline + tree\n")

	contentWidth := max(20, m.width-8)
	barWidth := max(6, contentWidth-4)

	maxRows := max(1, (height-2)/2)
	start, end := m.window(len(m.traceLines), m.traceCursor, maxRows)
	for i := start; i < end; i++ {
		line := m.traceLines[i]
		prefix := "  "
		if i == m.traceCursor {
			prefix = "> "
		}
		toggle := " "
		if line.HasKids {
			if line.Expanded {
				toggle = "-"
			} else {
				toggle = "+"
			}
		}
		errIcon := " "
		if line.Error {
			errIcon = "!"
		}
		linkIcon := " "
		if line.LinkCount > 0 {
			linkIcon = "@"
		}
		indent := strings.Repeat("  ", line.Depth)
		left := fmt.Sprintf("%s%s%s%s%s %s [%s] %s %s", prefix, indent, toggle, errIcon, linkIcon, m.spanIcon(line.Kind), line.Service, line.Label, line.Duration.Round(time.Microsecond))
		left = truncate(left, contentWidth)
		bar := m.timelineBar(line, barWidth)
		serviceStyle := colorForService(line.Service)
		b.WriteString(serviceStyle.Render(left))
		b.WriteString("\n")
		b.WriteString("    ")
		b.WriteString(serviceStyle.Render(bar))
		b.WriteString("\n")
	}
	if end == len(m.traceLines) {
		b.WriteString("    ")
		b.WriteString(strings.Repeat(" ", barWidth+1))
		b.WriteString(mutedStyle.Render("^ trace end"))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

func (m Model) serviceMapView(height int) string {
	if height < 2 {
		height = 2
	}
	lines := m.serviceMapLines()
	if len(lines) == 0 {
		return "Service map\n(no rows)"
	}
	maxRows := height - 1
	if maxRows < 1 {
		maxRows = 1
	}

	if len(lines) > maxRows {
		lines = append(lines[:maxRows-1], "(truncated; use fullscreen for more)")
	}

	var b strings.Builder
	b.WriteString("Service map\n")
	for _, line := range lines {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

func newValueView(title string, value any, width int) *valueView {
	text := fmt.Sprint(value)
	lines := wrapText(text, max(20, width))
	if len(lines) == 0 {
		lines = []string{""}
	}
	return &valueView{title: title, lines: lines}
}

func (m Model) valueViewView() string {
	if m.valueView == nil {
		return ""
	}
	maxRows := max(3, m.height-4)
	start := m.valueView.offset
	if start > len(m.valueView.lines)-1 {
		start = max(0, len(m.valueView.lines)-1)
	}
	end := min(len(m.valueView.lines), start+maxRows)

	var b strings.Builder
	b.WriteString(m.valueView.title)
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("esc/enter back"))
	b.WriteString("\n\n")
	for i := start; i < end; i++ {
		b.WriteString(m.valueView.lines[i])
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func wrapText(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	raw := strings.Split(text, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		r := []rune(line)
		if len(r) == 0 {
			out = append(out, "")
			continue
		}
		for len(r) > width {
			out = append(out, string(r[:width]))
			r = r[width:]
		}
		out = append(out, string(r))
	}
	return out
}

func (m Model) serviceMapLines() []string {
	edges := buildServiceEdges(m.session.Trace)
	services := serviceList(m.session.Trace)
	if len(services) == 0 {
		return []string{"(no service names on spans)"}
	}

	lines := make([]string, 0, len(edges)+4)
	lines = append(lines, "nodes: "+strings.Join(services, ", "))
	if len(edges) == 0 {
		return append(lines, "(no cross-service edges in this trace)")
	}
	lines = append(lines, "edges:")
	for _, edge := range edges {
		lines = append(lines, fmt.Sprintf("%s -> %s (%d calls)", edge.From, edge.To, edge.Count))
	}
	return lines
}

func (m Model) logsView(height int) string {
	var b strings.Builder
	cols := m.logColumns()
	widths := m.logColumnWidths(cols)

	b.WriteString("Logs\n")
	b.WriteString("  ")
	b.WriteString(m.renderLogRowHeader(cols, widths))
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(strings.Repeat("-", max(20, m.width-10)))
	b.WriteString("\n")

	if height < 4 {
		height = 4
	}
	rows := max(1, height-3)
	start, end := m.window(len(m.filteredLogs), m.logCursor, rows)
	for i := start; i < end; i++ {
		entry := m.filteredLogs[i]
		prefix := "  "
		if i == m.logCursor {
			prefix = "> "
		}
		row := prefix + m.renderLogRow(entry, cols, widths)
		b.WriteString(row)
		b.WriteString("\n")
	}

	if len(m.filteredLogs) == 0 {
		b.WriteString("(no logs in current level filter)")
	}

	return strings.TrimRight(b.String(), "\n")
}

func clampToHeight(view string, maxLines int) string {
	if maxLines <= 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	if len(lines) <= maxLines {
		return view
	}
	return strings.Join(lines[:maxLines], "\n")
}

func (m Model) logColumns() []logColumn {
	configured := m.cfg.UI.LogColumns
	if len(configured) == 0 {
		configured = []string{"timestamp", "service", "level", "message"}
	}
	cols := make([]logColumn, 0, len(configured))
	for _, raw := range configured {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		header, field, weight := splitColumnSpec(item)
		cols = append(cols, logColumn{Header: header, Field: field, Weight: weight})
	}
	if len(cols) == 0 {
		return []logColumn{{Header: "timestamp", Field: "timestamp", Weight: 1}, {Header: "service", Field: "service", Weight: 1}, {Header: "level", Field: "level", Weight: 1}, {Header: "message", Field: "message", Weight: 2}}
	}
	return cols
}

func splitColumnSpec(item string) (string, string, int) {
	for _, sep := range []string{"=", ":"} {
		if strings.Contains(item, sep) {
			parts := strings.SplitN(item, sep, 2)
			header := strings.TrimSpace(parts[0])
			field, weight := parseFieldAndWeight(strings.TrimSpace(parts[1]))
			if header == "" {
				header = field
			}
			return header, field, weight
		}
	}
	field, weight := parseFieldAndWeight(item)
	return field, field, weight
}

func parseFieldAndWeight(spec string) (string, int) {
	spec = strings.TrimSpace(spec)
	weight := 1
	if idx := strings.LastIndex(spec, "|"); idx > 0 && idx < len(spec)-1 {
		w := strings.TrimSpace(spec[idx+1:])
		if parsed, err := strconv.Atoi(w); err == nil && parsed > 0 {
			weight = parsed
			spec = strings.TrimSpace(spec[:idx])
		}
	}
	if spec == "" {
		spec = "message"
	}
	return spec, weight
}

func (m Model) logColumnWidths(cols []logColumn) []int {
	avail := max(20, m.width-8)
	if len(cols) == 0 {
		return []int{}
	}
	avail -= (len(cols) - 1) * 3
	if avail < len(cols)*4 {
		avail = len(cols) * 4
	}
	if len(cols) == 1 {
		return []int{avail}
	}

	weightTotal := 0
	for _, c := range cols {
		if c.Weight <= 0 {
			weightTotal++
			continue
		}
		weightTotal += c.Weight
	}
	if weightTotal <= 0 {
		weightTotal = len(cols)
	}

	widths := make([]int, len(cols))
	used := 0
	for i := range cols {
		w := cols[i].Weight
		if w <= 0 {
			w = 1
		}
		widths[i] = max(4, (avail*w)/weightTotal)
		used += widths[i]
	}

	if diff := avail - used; diff > 0 {
		for i := 0; i < diff; i++ {
			widths[i%len(widths)]++
		}
	} else if diff < 0 {
		need := -diff
		for need > 0 {
			changed := false
			for i := range widths {
				if widths[i] > 4 {
					widths[i]--
					need--
					changed = true
					if need == 0 {
						break
					}
				}
			}
			if !changed {
				break
			}
		}
	}

	return widths
}

func (m Model) renderLogRowHeader(cols []logColumn, widths []int) string {
	parts := make([]string, 0, len(cols))
	for i, col := range cols {
		parts = append(parts, padRight(col.Header, widths[i]))
	}
	return strings.Join(parts, " | ")
}

func (m Model) renderLogRow(entry domain.LogEntry, cols []logColumn, widths []int) string {
	parts := make([]string, 0, len(cols))
	for i, col := range cols {
		parts = append(parts, padRight(truncate(m.logFieldValue(entry, col.Field), widths[i]), widths[i]))
	}
	return strings.Join(parts, " | ")
}

func (m Model) logFieldValue(entry domain.LogEntry, field string) string {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "timestamp", "time":
		return entry.Timestamp.Format("15:04:05.000")
	case "service":
		return entry.Service
	case "level":
		return strings.ToUpper(entry.Level)
	case "message", "msg":
		return entry.Message
	case "raw":
		return entry.RawLine
	default:
		if strings.HasPrefix(strings.ToLower(field), "labels.") || strings.HasPrefix(strings.ToLower(field), "label.") {
			parts := strings.SplitN(field, ".", 2)
			if len(parts) == 2 {
				return entry.Labels[parts[1]]
			}
		}
		if v, ok := entry.Labels[field]; ok {
			return v
		}
		if entry.JSON == nil {
			return ""
		}
		if v, ok := nestedValue(entry.JSON, field); ok {
			return fmt.Sprint(v)
		}
		return ""
	}
}

func nestedValue(root map[string]any, path string) (any, bool) {
	if v, ok := root[path]; ok {
		return v, true
	}
	cur := any(root)
	for _, part := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := obj[part]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	return s + strings.Repeat(" ", width-len(r))
}

func labelsToObject(labels map[string]string) map[string]any {
	out := make(map[string]any, len(labels))
	for k, v := range labels {
		out[k] = v
	}
	return out
}

func (m Model) logDetailRoot(entry domain.LogEntry) OrderedRoot {
	parts := m.cfg.UI.LogDetailParts
	if len(parts) == 0 {
		parts = []string{"log", "labels", "raw"}
	}

	entries := make([]RootEntry, 0, len(parts))
	for _, part := range parts {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "log":
			if entry.JSON != nil {
				entries = append(entries, RootEntry{Key: "log", Value: entry.JSON})
			} else {
				entries = append(entries, RootEntry{Key: "log", Value: map[string]any{}})
			}
		case "labels":
			entries = append(entries, RootEntry{Key: "labels", Value: labelsToObject(entry.Labels)})
		case "raw":
			entries = append(entries, RootEntry{Key: "raw", Value: entry.RawLine})
		}
	}
	if len(entries) == 0 {
		entries = append(entries, RootEntry{Key: "log", Value: map[string]any{}})
		entries = append(entries, RootEntry{Key: "labels", Value: labelsToObject(entry.Labels)})
	}
	return OrderedRoot{Entries: entries}
}

func (m Model) helpView() string {
	sections := []struct {
		name string
		km   map[string][]string
	}{
		{name: "Global", km: m.cfg.Keymap.Global},
		{name: "Trace", km: m.cfg.Keymap.Trace},
		{name: "Logs", km: m.cfg.Keymap.Logs},
		{name: "JSON View", km: m.cfg.Keymap.JSON},
	}
	labels := map[string]string{
		"quit":              "Quit",
		"help":              "Toggle help",
		"back":              "Close current overlay",
		"switch_tab":        "Cycle section forward",
		"switch_tab_back":   "Cycle section backward",
		"toggle_fullscreen": "Toggle fullscreen section",
		"toggle_collapse":   "Collapse/expand section",
		"up":                "Move up",
		"down":              "Move down",
		"expand":            "Expand",
		"collapse":          "Collapse",
		"toggle":            "Toggle row",
		"details":           "Open details",
		"open_external":     "Open external URL",
		"level_up":          "Increase min level",
		"level_down":        "Decrease min level",
	}

	var b strings.Builder
	b.WriteString("Keyboard help (?)\n\n")
	for _, section := range sections {
		b.WriteString(section.name)
		b.WriteString("\n")
		actions := make([]string, 0, len(section.km))
		for action := range section.km {
			actions = append(actions, action)
		}
		sort.Strings(actions)
		for _, action := range actions {
			desc := labels[action]
			if desc == "" {
				desc = action
			}
			b.WriteString(fmt.Sprintf("  %-20s %s\n", strings.Join(section.km[action], ", "), desc))
		}
		b.WriteString("\n")
	}
	b.WriteString("Shortcuts are config-driven (config.json). esc/? close help; tab/shift+tab move sections.")

	return m.layout(b.String())
}

func (m Model) isAction(section, action, key string) bool {
	var km map[string][]string
	switch section {
	case "global":
		km = m.cfg.Keymap.Global
	case "trace":
		km = m.cfg.Keymap.Trace
	case "logs":
		km = m.cfg.Keymap.Logs
	case "json":
		km = m.cfg.Keymap.JSON
	default:
		return false
	}
	for _, k := range km[action] {
		if keysMatch(k, key) {
			return true
		}
	}
	return false
}

func keysMatch(configKey, pressed string) bool {
	if strings.EqualFold(configKey, pressed) {
		return true
	}
	normalize := func(v string) string {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "space", " ":
			return "space"
		case "esc", "escape":
			return "esc"
		default:
			return strings.ToLower(strings.TrimSpace(v))
		}
	}
	return normalize(configKey) == normalize(pressed)
}

func (m Model) currentTraceLine() *traceLine {
	if m.traceCursor < 0 || m.traceCursor >= len(m.traceLines) {
		return nil
	}
	line := m.traceLines[m.traceCursor]
	return &line
}

func (m Model) currentSpan() *domain.Span {
	line := m.currentTraceLine()
	if line == nil {
		return nil
	}
	return m.session.Trace.SpansByID[line.SpanID]
}

func (m Model) currentLog() (domain.LogEntry, bool) {
	if m.logCursor < 0 || m.logCursor >= len(m.filteredLogs) {
		return domain.LogEntry{}, false
	}
	return m.filteredLogs[m.logCursor], true
}

func (m Model) levelIndex(level string) int {
	for i, candidate := range m.cfg.Logs.LevelOrder {
		if strings.EqualFold(candidate, level) {
			return i
		}
	}
	return 0
}

func (m Model) timelineBar(line traceLine, width int) string {
	if width < 6 {
		width = 6
	}
	total := m.session.Trace.Duration
	if total <= 0 {
		return "|" + strings.Repeat(" ", width) + "|"
	}
	offset := line.Start.Sub(m.session.Trace.StartTime)
	if offset < 0 {
		offset = 0
	}
	left := int(float64(offset) / float64(total) * float64(width))
	spanWidth := int(float64(line.Duration) / float64(total) * float64(width))
	if spanWidth < 1 {
		spanWidth = 1
	}
	if left >= width {
		left = width - 1
	}
	if left+spanWidth > width {
		spanWidth = width - left
	}
	buf := make([]byte, width)
	for i := range buf {
		buf[i] = ' '
	}
	for i := left; i < left+spanWidth && i < len(buf); i++ {
		buf[i] = '='
	}
	return "|" + string(buf) + "|"
}

func (m Model) window(total, cursor, visible int) (int, int) {
	if visible < 1 {
		visible = 1
	}
	start := 0
	if cursor > visible-1 {
		start = cursor - (visible - 1)
	}
	end := min(total, start+visible)
	return start, end
}

func flattenTrace(trace *domain.Trace, expanded map[string]bool) []traceLine {
	var lines []traceLine
	for _, rootID := range trace.RootSpanIDs {
		span := trace.SpansByID[rootID]
		if span == nil {
			continue
		}
		walk(span, 0, expanded, &lines)
	}
	return lines
}

func walk(span *domain.Span, depth int, expanded map[string]bool, out *[]traceLine) {
	line := traceLine{
		SpanID:    span.ID,
		Depth:     depth,
		Label:     span.Name,
		Kind:      span.Kind,
		Service:   span.Service,
		HasKids:   len(span.Children) > 0,
		Expanded:  expanded[span.ID],
		Error:     span.HasError(),
		Duration:  span.Duration,
		Start:     span.Start,
		End:       span.End,
		LinkCount: len(span.Links),
	}
	*out = append(*out, line)
	if !expanded[span.ID] {
		return
	}
	for _, child := range span.Children {
		walk(child, depth+1, expanded, out)
	}
}

type serviceEdge struct {
	From  string
	To    string
	Count int
}

func buildServiceEdges(trace *domain.Trace) []serviceEdge {
	counts := map[string]int{}
	for _, span := range trace.Spans {
		if span.Service == "" {
			continue
		}
		for parent := trace.SpansByID[span.ParentID]; parent != nil; parent = trace.SpansByID[parent.ParentID] {
			if parent.Service == "" || parent.Service == span.Service {
				continue
			}
			key := parent.Service + "\x00" + span.Service
			counts[key]++
			break
		}
		for _, link := range span.Links {
			linked := trace.SpansByID[link.SpanID]
			if linked == nil || linked.Service == "" || linked.Service == span.Service {
				continue
			}
			key := span.Service + "\x00" + linked.Service
			counts[key]++
		}
	}
	if len(counts) == 0 {
		sorted := append([]*domain.Span(nil), trace.Spans...)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Start.Before(sorted[j].Start)
		})
		for i := 1; i < len(sorted); i++ {
			from := sorted[i-1].Service
			to := sorted[i].Service
			if from == "" || to == "" || from == to {
				continue
			}
			counts[from+"\x00"+to]++
		}
	}
	out := make([]serviceEdge, 0, len(counts))
	for key, count := range counts {
		parts := strings.Split(key, "\x00")
		out = append(out, serviceEdge{From: parts[0], To: parts[1], Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From == out[j].From {
			return out[i].To < out[j].To
		}
		return out[i].From < out[j].From
	})
	return out
}

func serviceList(trace *domain.Trace) []string {
	set := map[string]struct{}{}
	for _, span := range trace.Spans {
		if span.Service != "" {
			set[span.Service] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func (m Model) traceDetailRoot(span *domain.Span) OrderedRoot {
	parts := m.cfg.UI.TraceDetailParts
	if len(parts) == 0 {
		parts = []string{"metadata", "attributes", "events", "links"}
	}

	entries := make([]RootEntry, 0, len(parts))
	for _, part := range parts {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "metadata":
			entries = append(entries, RootEntry{Key: "metadata", Value: spanMetadataPayload(span)})
		case "attributes":
			entries = append(entries, RootEntry{Key: "attributes", Value: span.Attributes})
		case "events":
			entries = append(entries, RootEntry{Key: "events", Value: spanEventsPayload(span)})
		case "links":
			entries = append(entries, RootEntry{Key: "links", Value: spanLinksPayload(span.Links)})
		}
	}
	if len(entries) == 0 {
		entries = append(entries, RootEntry{Key: "metadata", Value: spanMetadataPayload(span)})
		entries = append(entries, RootEntry{Key: "attributes", Value: span.Attributes})
		entries = append(entries, RootEntry{Key: "events", Value: spanEventsPayload(span)})
		entries = append(entries, RootEntry{Key: "links", Value: spanLinksPayload(span.Links)})
	}
	return OrderedRoot{Entries: entries}
}

func spanMetadataPayload(span *domain.Span) map[string]any {
	return map[string]any{
		"id":        span.ID,
		"parent_id": span.ParentID,
		"kind":      span.Kind,
		"service":   span.Service,
		"duration":  span.Duration.String(),
		"status":    span.StatusCode,
		"error":     span.HasError(),
	}
}

func spanEventsPayload(span *domain.Span) map[string]any {
	events := map[string]any{}
	for i, event := range span.Events {
		key := fmt.Sprintf("%02d %s @ %s", i+1, event.Name, event.Time.Format("15:04:05.000"))
		events[key] = event.Attributes
	}
	return events
}

func spanLinksPayload(links []domain.SpanLink) map[string]any {
	out := map[string]any{}
	for i, link := range links {
		key := fmt.Sprintf("%02d %s -> %s", i+1, shortID(link.TraceID), shortID(link.SpanID))
		out[key] = map[string]any{
			"trace_id":   link.TraceID,
			"span_id":    link.SpanID,
			"attributes": link.Attributes,
		}
	}
	return out
}

func (m Model) spanIcon(kind string) string {
	icons := m.cfg.UI.SpanIcons
	k := strings.ToLower(kind)
	switch {
	case strings.Contains(k, "server"):
		return iconOrDefault(icons, "server", "[srv]")
	case strings.Contains(k, "client"):
		return iconOrDefault(icons, "client", "[cli]")
	case strings.Contains(k, "producer"):
		return iconOrDefault(icons, "producer", "[prd]")
	case strings.Contains(k, "consumer"):
		return iconOrDefault(icons, "consumer", "[con]")
	default:
		return iconOrDefault(icons, "internal", "[int]")
	}
}

func iconOrDefault(icons map[string]string, key, fallback string) string {
	if icons == nil {
		return fallback
	}
	if v, ok := icons[key]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

func sectionStyle(active bool, width int) lipgloss.Style {
	color := lipgloss.Color("240")
	if active {
		color = lipgloss.Color("33")
	}
	return lipgloss.NewStyle().Width(width).Border(lipgloss.NormalBorder()).BorderForeground(color).Padding(0, 1)
}

func colorForService(service string) lipgloss.Style {
	palette := []string{"69", "75", "81", "111", "174", "208", "179", "141"}
	if service == "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	}
	h := 0
	for _, r := range service {
		h += int(r)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(palette[h%len(palette)]))
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
