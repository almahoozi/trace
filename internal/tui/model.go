package tui

import (
	"fmt"
	"net/url"
	"regexp"
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
	XCost     time.Duration
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
	loc     *time.Location

	width  int
	height int

	activePanel panel
	showHelp    bool
	configView  *ConfigModel
	jsonTree    *JSONTree
	valueView   *valueView
	fullscreen  bool
	collapsed   map[panel]bool

	traceLines     []traceLine
	traceCursor    int
	expanded       map[string]bool
	serviceMapTree *JSONTree
	serviceColors  map[string]string

	allLogs          []domain.LogEntry
	levelFilteredLog []domain.LogEntry
	filteredLogs     []domain.LogEntry
	logCursor        int
	levelThresholdIx int
	logSearchRaw     string
	logSearchMatcher *searchMatcher
	lastSearchRaw    string
	lastSearch       *searchMatcher
	pendingGG        bool

	searchPrompt *searchPrompt

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
		loc:          time.Local,
		expanded:     map[string]bool{},
		collapsed:    map[panel]bool{},
		fullscreen:   cfg.UI.DefaultFullscreen,
		allLogs:      session.Logs,
		filteredLogs: session.Logs,
		status:       fmt.Sprintf("env=%s spans=%d logs=%d", session.Environment, len(session.Trace.Spans), len(session.Logs)),
	}
	if loc, err := cfg.DisplayLocation(); err == nil {
		m.loc = loc
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
	m.serviceMapTree = m.newServiceMapTree()
	m.initServiceColors()
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
		if m.configView != nil {
			updated, cmd := m.configView.Update(msg)
			if next, ok := updated.(ConfigModel); ok {
				m.configView = &next
			}
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		keyMsg := msg
		key := keyMsg.String()

		if m.searchPrompt != nil {
			return m.updateSearchPrompt(keyMsg)
		}

		if m.isAction("global", "quit", key) {
			return m, tea.Quit
		}
		if isConfigHotkey(key) {
			m.openConfigMode()
			return m, nil
		}
		if m.configView != nil {
			if m.isAction("global", "back", key) && m.configView.AtRoot() {
				m.configView = nil
				m.reloadConfig()
				m.status = "closed config mode"
				return m, nil
			}
			updated, cmd := m.configView.Update(msg)
			if next, ok := updated.(ConfigModel); ok {
				m.configView = &next
			}
			return m, cmd
		}

		if m.showHelp {
			if m.isAction("global", "help", key) || m.isAction("global", "back", key) || strings.EqualFold(key, "esc") {
				m.showHelp = false
			}
			return m, nil
		}

		if key == "/" {
			m.openSearchPrompt()
			return m, nil
		}

		if m.isAction("global", "help", key) {
			m.showHelp = true
			return m, nil
		}

		if m.consumeGGPrefix(key) {
			return m, nil
		}
		if m.handleTopBottomShortcut(key) {
			return m, nil
		}
		if m.handleSearchRepeatShortcut(key) {
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
	if isPageDownKey(key) {
		for i := 0; i < m.jsonPageRows(); i++ {
			m.jsonTree.MoveDown()
		}
	}
	if isPageUpKey(key) {
		for i := 0; i < m.jsonPageRows(); i++ {
			m.jsonTree.MoveUp()
		}
	}
	if isHalfPageDownKey(key) {
		for i := 0; i < m.jsonHalfPageRows(); i++ {
			m.jsonTree.MoveDown()
		}
	}
	if isHalfPageUpKey(key) {
		for i := 0; i < m.jsonHalfPageRows(); i++ {
			m.jsonTree.MoveUp()
		}
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
	if isPageDownKey(key) && len(m.valueView.lines) > 0 {
		m.valueView.offset = min(len(m.valueView.lines)-1, m.valueView.offset+m.valuePageRows())
	}
	if isPageUpKey(key) && len(m.valueView.lines) > 0 {
		m.valueView.offset = max(0, m.valueView.offset-m.valuePageRows())
	}
	if isHalfPageDownKey(key) && len(m.valueView.lines) > 0 {
		m.valueView.offset = min(len(m.valueView.lines)-1, m.valueView.offset+m.valueHalfPageRows())
	}
	if isHalfPageUpKey(key) && len(m.valueView.lines) > 0 {
		m.valueView.offset = max(0, m.valueView.offset-m.valueHalfPageRows())
	}
	return m, nil
}

func (m Model) updateSearchPrompt(keyMsg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchPrompt == nil {
		return m, nil
	}
	switch keyMsg.String() {
	case "esc":
		m.searchPrompt = nil
		m.status = "search cancelled"
		return m, nil
	case "enter":
		raw := strings.TrimSpace(m.searchPrompt.value())
		m.searchPrompt = nil
		m.applySearch(raw)
		return m, nil
	}
	if m.searchPrompt.applyKey(keyMsg) {
		return m, nil
	}
	return m, nil
}

func (m *Model) openSearchPrompt() {
	initial := ""
	if m.jsonTree == nil && m.valueView == nil {
		initial = m.logSearchRaw
	}
	m.searchPrompt = newSearchPrompt(initial)
	m.status = "search: type query and press enter"
}

func (m *Model) applySearch(raw string) {
	matcher, err := compileSearchMatcher(raw)
	if err != nil {
		m.status = "search parse failed: " + err.Error()
		return
	}
	m.lastSearchRaw = raw
	m.lastSearch = matcher

	if m.valueView != nil {
		m.applyValueSearch(matcher, raw, 1)
		return
	}
	if m.jsonTree != nil {
		m.applyJSONSearch(matcher, raw, 1)
		return
	}

	switch m.activePanel {
	case panelTrace:
		m.applyTraceSearch(matcher, raw, 1)
	case panelServiceMap:
		m.applyServiceMapSearch(matcher, raw, 1)
	default:
		m.applyLogSearch(matcher, raw)
	}
}

func (m *Model) consumeGGPrefix(key string) bool {
	if m.pendingGG {
		m.pendingGG = false
		if isTopPrefixKey(key) {
			m.moveToTop()
			return true
		}
	}
	if isTopPrefixKey(key) {
		m.pendingGG = true
		return true
	}
	return false
}

func (m *Model) handleTopBottomShortcut(key string) bool {
	if isBottomShortcutKey(key) {
		m.moveToBottom()
		return true
	}
	return false
}

func (m *Model) handleSearchRepeatShortcut(key string) bool {
	direction := 0
	if isSearchNextKey(key) {
		direction = 1
	}
	if isSearchPrevKey(key) {
		direction = -1
	}
	if direction == 0 {
		return false
	}
	if m.lastSearch == nil {
		m.status = "no active search"
		return true
	}
	if m.valueView != nil {
		m.applyValueSearch(m.lastSearch, m.lastSearchRaw, direction)
		return true
	}
	if m.jsonTree != nil {
		m.applyJSONSearch(m.lastSearch, m.lastSearchRaw, direction)
		return true
	}
	switch m.activePanel {
	case panelTrace:
		m.applyTraceSearch(m.lastSearch, m.lastSearchRaw, direction)
	case panelServiceMap:
		m.applyServiceMapSearch(m.lastSearch, m.lastSearchRaw, direction)
	default:
		m.applyLogSearch(m.lastSearch, m.lastSearchRaw)
		if len(m.filteredLogs) == 0 {
			m.status = fmt.Sprintf("log search %q: no match", m.lastSearchRaw)
			return true
		}
		if direction > 0 {
			m.logCursor = (m.logCursor + 1) % len(m.filteredLogs)
		} else {
			m.logCursor = (m.logCursor - 1 + len(m.filteredLogs)) % len(m.filteredLogs)
		}
		m.status = fmt.Sprintf("log search %q -> row %d/%d", m.lastSearchRaw, m.logCursor+1, len(m.filteredLogs))
	}
	return true
}

func (m *Model) moveToTop() {
	if m.valueView != nil {
		m.valueView.offset = 0
		return
	}
	if m.jsonTree != nil {
		if len(m.jsonTree.lines) > 0 {
			m.jsonTree.cursor = 0
		}
		return
	}
	switch m.activePanel {
	case panelTrace:
		m.traceCursor = 0
	case panelServiceMap:
		if m.serviceMapTree != nil {
			m.serviceMapTree.cursor = 0
		}
	default:
		m.logCursor = 0
	}
}

func (m *Model) moveToBottom() {
	if m.valueView != nil {
		if len(m.valueView.lines) > 0 {
			m.valueView.offset = len(m.valueView.lines) - 1
		}
		return
	}
	if m.jsonTree != nil {
		if len(m.jsonTree.lines) > 0 {
			m.jsonTree.cursor = len(m.jsonTree.lines) - 1
		}
		return
	}
	switch m.activePanel {
	case panelTrace:
		if len(m.traceLines) > 0 {
			m.traceCursor = len(m.traceLines) - 1
		}
	case panelServiceMap:
		if m.serviceMapTree != nil && len(m.serviceMapTree.lines) > 0 {
			m.serviceMapTree.cursor = len(m.serviceMapTree.lines) - 1
		}
	default:
		if len(m.filteredLogs) > 0 {
			m.logCursor = len(m.filteredLogs) - 1
		}
	}
}

func (m Model) updateTrace(key string) (tea.Model, tea.Cmd) {
	if m.isAction("trace", "up", key) && m.traceCursor > 0 {
		m.traceCursor--
	}
	if m.isAction("trace", "down", key) && m.traceCursor < len(m.traceLines)-1 {
		m.traceCursor++
	}
	if isPageDownKey(key) && len(m.traceLines) > 0 {
		m.traceCursor = min(len(m.traceLines)-1, m.traceCursor+m.tracePageRows())
	}
	if isPageUpKey(key) && len(m.traceLines) > 0 {
		m.traceCursor = max(0, m.traceCursor-m.tracePageRows())
	}
	if isHalfPageDownKey(key) && len(m.traceLines) > 0 {
		m.traceCursor = min(len(m.traceLines)-1, m.traceCursor+m.traceHalfPageRows())
	}
	if isHalfPageUpKey(key) && len(m.traceLines) > 0 {
		m.traceCursor = max(0, m.traceCursor-m.traceHalfPageRows())
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

func (m Model) updateServiceMap(key string) (tea.Model, tea.Cmd) {
	if m.serviceMapTree == nil {
		m.serviceMapTree = m.newServiceMapTree()
	}
	if len(m.serviceMapTree.lines) == 0 {
		m.serviceMapTree.cursor = 0
		return m, nil
	}
	if m.isAction("trace", "up", key) {
		m.serviceMapTree.MoveUp()
	}
	if m.isAction("trace", "down", key) {
		m.serviceMapTree.MoveDown()
	}
	if isPageDownKey(key) {
		for i := 0; i < m.serviceMapPageRows(); i++ {
			m.serviceMapTree.MoveDown()
		}
	}
	if isPageUpKey(key) {
		for i := 0; i < m.serviceMapPageRows(); i++ {
			m.serviceMapTree.MoveUp()
		}
	}
	if isHalfPageDownKey(key) {
		for i := 0; i < m.serviceMapHalfPageRows(); i++ {
			m.serviceMapTree.MoveDown()
		}
	}
	if isHalfPageUpKey(key) {
		for i := 0; i < m.serviceMapHalfPageRows(); i++ {
			m.serviceMapTree.MoveUp()
		}
	}
	if m.isAction("json", "expand", key) {
		m.serviceMapTree.Expand()
	}
	if m.isAction("json", "collapse", key) {
		m.serviceMapTree.Collapse()
	}
	if m.isAction("json", "toggle", key) || key == " " {
		m.serviceMapTree.Toggle()
	}
	return m, nil
}

func (m Model) updateLogs(key string) (tea.Model, tea.Cmd) {
	if m.isAction("logs", "up", key) && m.logCursor > 0 {
		m.logCursor--
	}
	if m.isAction("logs", "down", key) && m.logCursor < len(m.filteredLogs)-1 {
		m.logCursor++
	}
	if isPageDownKey(key) && len(m.filteredLogs) > 0 {
		m.logCursor = min(len(m.filteredLogs)-1, m.logCursor+m.logsPageRows())
	}
	if isPageUpKey(key) && len(m.filteredLogs) > 0 {
		m.logCursor = max(0, m.logCursor-m.logsPageRows())
	}
	if isHalfPageDownKey(key) && len(m.filteredLogs) > 0 {
		m.logCursor = min(len(m.filteredLogs)-1, m.logCursor+m.logsHalfPageRows())
	}
	if isHalfPageUpKey(key) && len(m.filteredLogs) > 0 {
		m.logCursor = max(0, m.logCursor-m.logsHalfPageRows())
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
		m.levelFilteredLog = m.allLogs
		m.applyLogSearchFilter()
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
	m.levelFilteredLog = filtered
	m.applyLogSearchFilter()
	m.status = fmt.Sprintf("log filter level >= %s (%d lines)", threshold, len(m.filteredLogs))
}

func (m *Model) applyLogSearchFilter() {
	if m.logSearchMatcher == nil {
		m.filteredLogs = m.levelFilteredLog
	} else {
		filtered := make([]domain.LogEntry, 0, len(m.levelFilteredLog))
		for _, entry := range m.levelFilteredLog {
			if m.logSearchMatcher.MatchFields(logSearchFields(entry), logSearchBlob(entry)) {
				filtered = append(filtered, entry)
			}
		}
		m.filteredLogs = filtered
	}
	if m.logCursor >= len(m.filteredLogs) {
		m.logCursor = max(0, len(m.filteredLogs)-1)
	}
}

func (m *Model) applyLogSearch(matcher *searchMatcher, raw string) {
	m.logSearchRaw = raw
	m.logSearchMatcher = matcher
	m.applyLogThreshold()
	if matcher == nil {
		m.status = fmt.Sprintf("log search cleared (%d lines)", len(m.filteredLogs))
		return
	}
	m.status = fmt.Sprintf("log search %q matched %d lines", raw, len(m.filteredLogs))
}

func (m *Model) applyTraceSearch(matcher *searchMatcher, raw string, direction int) {
	if matcher == nil {
		m.status = "trace search cleared"
		return
	}
	if len(m.traceLines) == 0 {
		m.status = "trace search: no spans"
		return
	}
	if direction == 0 {
		direction = 1
	}
	start := m.traceCursor + direction
	for i := 0; i < len(m.traceLines); i++ {
		idx := (start + i*direction + len(m.traceLines)*2) % len(m.traceLines)
		line := m.traceLines[idx]
		span := m.session.Trace.SpansByID[line.SpanID]
		fields := traceSearchFields(line, span)
		blob := traceSearchBlob(line, span)
		if matcher.MatchFields(fields, blob) {
			m.traceCursor = idx
			m.status = fmt.Sprintf("trace search %q -> row %d/%d", raw, idx+1, len(m.traceLines))
			return
		}
	}
	m.status = fmt.Sprintf("trace search %q: no match", raw)
}

func (m *Model) applyServiceMapSearch(matcher *searchMatcher, raw string, direction int) {
	if m.serviceMapTree == nil {
		m.serviceMapTree = m.newServiceMapTree()
	}
	lines := m.serviceMapLines()
	if matcher == nil {
		m.status = "service map search cleared"
		return
	}
	if len(lines) == 0 {
		m.status = "service map search: no rows"
		return
	}
	if direction == 0 {
		direction = 1
	}
	start := m.serviceMapTree.cursor + direction
	for i := 0; i < len(lines); i++ {
		idx := (start + i*direction + len(lines)*2) % len(lines)
		fields := map[string]string{"line": lines[idx]}
		if matcher.MatchFields(fields, lines[idx]) {
			m.serviceMapTree.cursor = idx
			m.status = fmt.Sprintf("service map search %q -> row %d/%d", raw, idx+1, len(lines))
			return
		}
	}
	m.status = fmt.Sprintf("service map search %q: no match", raw)
}

func (m *Model) applyJSONSearch(matcher *searchMatcher, raw string, direction int) {
	if matcher == nil {
		m.status = "json search cleared"
		return
	}
	if m.jsonTree == nil {
		m.status = "json search unavailable"
		return
	}
	matched := false
	if direction < 0 {
		matched = m.jsonTree.SearchPrev(matcher)
	} else {
		matched = m.jsonTree.SearchNext(matcher)
	}
	if matched {
		m.status = fmt.Sprintf("json search %q matched", raw)
		return
	}
	m.status = fmt.Sprintf("json search %q: no match", raw)
}

func (m *Model) applyValueSearch(matcher *searchMatcher, raw string, direction int) {
	if m.valueView == nil {
		return
	}
	if matcher == nil {
		m.status = "value search cleared"
		return
	}
	if len(m.valueView.lines) == 0 {
		m.status = "value search: no lines"
		return
	}
	if direction == 0 {
		direction = 1
	}
	start := m.valueView.offset + direction
	for i := 0; i < len(m.valueView.lines); i++ {
		idx := (start + i*direction + len(m.valueView.lines)*2) % len(m.valueView.lines)
		line := m.valueView.lines[idx]
		fields := map[string]string{"line": line}
		if matcher.MatchFields(fields, line) {
			m.valueView.offset = idx
			m.status = fmt.Sprintf("value search %q -> line %d/%d", raw, idx+1, len(m.valueView.lines))
			return
		}
	}
	m.status = fmt.Sprintf("value search %q: no match", raw)
}

func isTopPrefixKey(key string) bool {
	return key == "g"
}

func isBottomShortcutKey(key string) bool {
	return key == "G" || key == "shift+g"
}

func isSearchNextKey(key string) bool {
	return key == "n"
}

func isSearchPrevKey(key string) bool {
	return key == "N" || key == "shift+n"
}

func isPageDownKey(key string) bool {
	return keysMatch("pgdown", key) || keysMatch("ctrl+f", key)
}

func isPageUpKey(key string) bool {
	return keysMatch("pgup", key) || keysMatch("ctrl+b", key)
}

func isHalfPageDownKey(key string) bool {
	return keysMatch("ctrl+d", key)
}

func isHalfPageUpKey(key string) bool {
	return keysMatch("ctrl+u", key)
}

func (m Model) tracePageRows() int {
	return max(1, m.traceVisibleRows())
}

func (m Model) traceHalfPageRows() int {
	return max(1, m.traceVisibleRows()/2)
}

func (m Model) logsPageRows() int {
	return max(1, m.logsVisibleRows())
}

func (m Model) logsHalfPageRows() int {
	return max(1, m.logsVisibleRows()/2)
}

func (m Model) jsonPageRows() int {
	return max(1, m.height-5)
}

func (m Model) jsonHalfPageRows() int {
	return max(1, m.jsonPageRows()/2)
}

func (m Model) valuePageRows() int {
	return max(1, m.height-5)
}

func (m Model) valueHalfPageRows() int {
	return max(1, m.valuePageRows()/2)
}

func (m Model) serviceMapPageRows() int {
	return max(1, m.serviceMapVisibleRows())
}

func (m Model) serviceMapHalfPageRows() int {
	return max(1, m.serviceMapVisibleRows()/2)
}

func (m Model) traceVisibleRows() int {
	if m.fullscreen && m.activePanel == panelTrace {
		innerHeight := max(4, m.height-4)
		return max(1, (innerHeight-2)/2)
	}
	return max(1, (max(4, m.height/3)-2)/2)
}

func (m Model) logsVisibleRows() int {
	if m.fullscreen && m.activePanel == panelLogs {
		innerHeight := max(4, m.height-4)
		return max(1, innerHeight-3)
	}
	return max(1, max(4, m.height/3)-3)
}

func (m Model) serviceMapVisibleRows() int {
	if m.fullscreen && m.activePanel == panelServiceMap {
		innerHeight := max(3, m.height-4)
		return max(1, innerHeight-1)
	}
	return max(1, max(3, m.height/3)-1)
}

func logSearchFields(entry domain.LogEntry) map[string]string {
	fields := map[string]string{
		"timestamp": entry.Timestamp.Format(time.RFC3339Nano),
		"service":   entry.Service,
		"level":     entry.Level,
		"message":   entry.Message,
		"raw":       entry.RawLine,
	}
	for k, v := range entry.Labels {
		fields["labels."+k] = v
	}
	if entry.JSON != nil {
		flattenAny("", entry.JSON, fields)
	}
	return fields
}

func logSearchBlob(entry domain.LogEntry) string {
	parts := []string{entry.Service, entry.Level, entry.Message, entry.RawLine}
	for k, v := range entry.Labels {
		parts = append(parts, k, v)
	}
	return strings.Join(parts, " ")
}

func traceSearchFields(line traceLine, span *domain.Span) map[string]string {
	fields := map[string]string{
		"span.id":   line.SpanID,
		"span.name": line.Label,
		"service":   line.Service,
		"kind":      line.Kind,
		"error":     strconv.FormatBool(line.Error),
	}
	if span != nil {
		fields["status"] = span.StatusCode
		fields["status_message"] = span.StatusMsg
		flattenAny("", span.Attributes, fields)
	}
	return fields
}

func traceSearchBlob(line traceLine, span *domain.Span) string {
	parts := []string{line.SpanID, line.Service, line.Label, line.Kind}
	if span != nil {
		parts = append(parts, span.StatusCode, span.StatusMsg)
	}
	return strings.Join(parts, " ")
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading..."
	}
	if m.configView != nil {
		return clampToHeight(m.configView.View(), m.height)
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

	headerLine1 := m.summaryHeaderLine()
	headerLine2 := m.summaryStatsLine()
	headerRendered := lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.NewStyle().Width(max(1, m.width)).Render(headerLine1),
		lipgloss.NewStyle().Width(max(1, m.width)).Render(headerLine2),
	)
	headerHeight := max(1, lipgloss.Height(headerRendered))

	if m.fullscreen {
		innerHeight := max(1, m.height-headerHeight-2)
		body := sectionStyle(true, m.width, innerHeight).Render(m.panelView(m.activePanel, innerHeight))
		footer := mutedStyle.Render(m.status + " | / search | n/N next/prev | gg/G top/bottom | ctrl+f/b page | ctrl+d/u half-page | f fullscreen")
		if m.searchPrompt != nil {
			footer += "\n" + mutedStyle.Render(m.searchPrompt.viewLine()) + "\n" + mutedStyle.Render(searchHint())
		}
		return clampToHeight(lipgloss.JoinVertical(lipgloss.Left, headerRendered, body, footer), m.height)
	}

	footer := mutedStyle.Render(m.status + " | / search | n/N next/prev | gg/G top/bottom | ctrl+f/b page | ctrl+d/u half-page | f fullscreen | c collapse | tab/shift+tab switch | F2 config | ? help")
	if m.searchPrompt != nil {
		footer += "\n" + mutedStyle.Render(m.searchPrompt.viewLine()) + "\n" + mutedStyle.Render(searchHint())
	}
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
			rendered = append(rendered, sectionStyle(m.activePanel == p, m.width, max(1, innerHeights[p])).Render(panelTitle(p)+" (collapsed)"))
			continue
		}
		rendered = append(rendered, sectionStyle(m.activePanel == p, m.width, max(1, innerHeights[p])).Render(m.panelView(p, max(1, innerHeights[p]))))
	}

	return clampToHeight(lipgloss.JoinVertical(lipgloss.Left, append([]string{headerRendered}, append(rendered, footer)...)...), m.height)
}

func (m Model) summaryHeaderLine() string {
	trace := m.session.Trace
	if trace == nil {
		return "[unknown]"
	}
	parts := []string{
		summaryBrightStyle.Render("[") + summaryGrayStyle.Render(m.session.Environment) + summaryBrightStyle.Render("]"),
	}
	if trace.TraceID != "" {
		parts = append(parts, summaryBrightStyle.Render(trace.TraceID))
	}
	if trace.ErrorSpanCount > 0 {
		parts = append(parts, summaryErrorStyle.Render("!"))
	}
	if status, ok := m.rootHTTPStatus(); ok {
		parts = append(parts, summaryHTTPStatusStyle(status).Render(fmt.Sprintf("%d", status)))
	}
	operation := strings.TrimSpace(trace.OperationName)
	if operation == "" {
		operation = "-"
	}
	parts = append(parts, summaryBrightStyle.Render(operation))
	parts = append(parts,
		summaryBrightStyle.Render("(")+summaryDurationStyle(trace.Duration).Render(trace.Duration.Round(time.Millisecond).String())+summaryBrightStyle.Render(")"),
	)
	if !trace.StartTime.IsZero() {
		start := trace.StartTime.In(m.loc).Format("2006-01-02 15:04:05.000")
		end := trace.StartTime.Add(trace.Duration).In(m.loc).Format("2006-01-02 15:04:05.000")
		parts = append(parts,
			summaryBrightStyle.Render("-"),
			summaryBrightStyle.Render("[")+summaryGrayStyle.Render(start)+summaryBrightStyle.Render(" - ")+summaryGrayStyle.Render(end)+summaryBrightStyle.Render("]"),
		)
	}
	return strings.Join(parts, " ")
}

func summaryHTTPStatusStyle(status int) lipgloss.Style {
	switch status / 100 {
	case 2:
		return summarySuccessStyle
	case 3:
		return summaryInfoStyle
	case 4:
		return summaryWarnStyle
	case 5:
		return summaryErrorStyle
	default:
		return summaryBrightStyle
	}
}

func summaryDurationStyle(d time.Duration) lipgloss.Style {
	switch {
	case d < 100*time.Millisecond:
		return summarySuccessStyle
	case d < time.Second:
		return summaryBrightStyle
	case d < 3*time.Second:
		return summaryWarnStyle
	default:
		return summaryErrorStyle
	}
}

func (m Model) rootHTTPStatus() (int, bool) {
	trace := m.session.Trace
	if trace == nil {
		return 0, false
	}
	var root *domain.Span
	for _, rootID := range trace.RootSpanIDs {
		if span := trace.SpansByID[rootID]; span != nil {
			root = span
			break
		}
	}
	if root == nil && len(trace.Spans) > 0 {
		root = trace.Spans[0]
	}
	if root == nil || root.Attributes == nil {
		return 0, false
	}
	v, ok := root.Attributes["http.response.status_code"]
	if !ok {
		return 0, false
	}
	s, ok := toInt(v)
	if !ok || s <= 0 {
		return 0, false
	}
	return s, true
}

func toInt(v any) (int, bool) {
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func (m Model) logsHeaderDate() string {
	if entry, ok := m.currentLog(); ok {
		return entry.Timestamp.In(m.loc).Format("2006-01-02")
	}
	if m.session != nil && m.session.Trace != nil && !m.session.Trace.StartTime.IsZero() {
		return m.session.Trace.StartTime.In(m.loc).Format("2006-01-02")
	}
	return ""
}

func (m Model) summaryStatsLine() string {
	parts := []string{
		summaryGrayStyle.Render("services") + summaryGrayStyle.Render(":") + " " + summaryBrightStyle.Render(fmt.Sprintf("%d", m.session.Trace.ServiceCount)),
		summaryGrayStyle.Render("errors") + summaryGrayStyle.Render(":") + " " + summaryBrightStyle.Render(fmt.Sprintf("%d", m.session.Trace.ErrorSpanCount)),
		summaryGrayStyle.Render("spans") + summaryGrayStyle.Render(":") + " " + summaryBrightStyle.Render(fmt.Sprintf("%d", m.session.Trace.SpanCount)),
		summaryGrayStyle.Render("logs") + summaryGrayStyle.Render(":") + " " + summaryBrightStyle.Render(fmt.Sprintf("%d", len(m.filteredLogs))),
	}
	return strings.Join(parts, "  ")
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
	footer := mutedStyle.Render(m.status + " | / search | n/N next/prev | gg/G top/bottom | ctrl+f/b page | ctrl+d/u half-page | ? help | esc back")
	if m.searchPrompt != nil {
		footer += "\n" + mutedStyle.Render(m.searchPrompt.viewLine()) + "\n" + mutedStyle.Render(searchHint())
	}
	return lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render("trace viewer"), body, footer)
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
		durationInfo := formatDurationDisplay(line.Duration)
		if line.HasKids {
			durationInfo += " [" + formatDurationDisplay(line.XCost) + "]"
		}
		left := fmt.Sprintf("%s%s%s%s%s %s [%s] %s %s", prefix, indent, toggle, errIcon, linkIcon, m.spanIcon(line.Kind), line.Service, line.Label, durationInfo)
		left = truncate(left, contentWidth)
		bar := m.timelineBar(line, barWidth)
		serviceStyle := m.colorForService(line.Service)
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
	if m.serviceMapTree == nil {
		m.serviceMapTree = m.newServiceMapTree()
	}
	if m.serviceMapTree == nil || len(m.serviceMapTree.lines) == 0 {
		return "Service map\n(no rows)"
	}
	return m.serviceMapTree.View(height)
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
	if m.serviceMapTree == nil {
		return nil
	}
	lines := make([]string, 0, len(m.serviceMapTree.lines))
	for _, line := range m.serviceMapTree.lines {
		lines = append(lines, line.Label)
	}
	return lines
}

func (m Model) newServiceMapTree() *JSONTree {
	root := m.serviceMapRoot()
	paths := m.serviceMapExpandedPaths(root)
	return NewJSONTreeWithExpanded("Service map", root, paths...)
}

func (m Model) serviceMapExpandedPaths(root OrderedRoot) []string {
	expanded := map[string]bool{}
	for _, entry := range root.Entries {
		if entry.Key != "map" {
			continue
		}
		collectExpandablePaths("$.map", entry.Value, expanded)
		break
	}
	if len(expanded) == 0 {
		expanded["$.map"] = true
	}
	paths := make([]string, 0, len(expanded))
	for path := range expanded {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (m Model) serviceMapRoot() OrderedRoot {
	services := serviceList(m.session.Trace)
	if len(services) == 0 {
		return OrderedRoot{Entries: []RootEntry{{Key: "message", Value: "(no service names on spans)"}}}
	}

	totalCost := requestTotalCost(m.session.Trace)
	nodeCosts := serviceNodeCosts(m.session.Trace)
	edges := buildServiceEdges(m.session.Trace)
	externals := buildExternalDependencies(m.session.Trace)
	edgesByFrom := map[string][]serviceEdge{}
	incoming := map[string]int{}
	for _, edge := range edges {
		edgesByFrom[edge.FromService] = append(edgesByFrom[edge.FromService], edge)
		incoming[edge.ToService]++
	}
	allNodeCosts := serviceMapNodeCostsAll(m.session.Trace)
	nodeNames := make([]string, 0, len(allNodeCosts))
	nodeEntries := make([]RootEntry, 0, len(allNodeCosts))
	for nodeName := range allNodeCosts {
		nodeNames = append(nodeNames, nodeName)
	}
	sort.Slice(nodeNames, func(i, j int) bool {
		iProxy := strings.HasSuffix(nodeNames[i], " [P]")
		jProxy := strings.HasSuffix(nodeNames[j], " [P]")
		if iProxy != jProxy {
			return !iProxy
		}
		return nodeNames[i] < nodeNames[j]
	})
	for _, nodeName := range nodeNames {
		isProxyNode := strings.HasSuffix(nodeName, " [P]")
		key := m.renderNodeSummaryLabel(nodeName, isProxyNode)
		nodeEntries = append(nodeEntries, RootEntry{Key: key, Value: formatDurationDisplay(allNodeCosts[nodeName])})
	}

	edgesSummary := map[string]any{}
	for _, edge := range edges {
		key := m.renderEntityLabel(edge.FromService, edge.FromSidecar, "service") + " -> " + m.renderEntityLabel(edge.ToService, edge.ToSidecar, "service")
		edgesSummary[key] = "x" + strconv.Itoa(edge.Count)
	}
	externalsByFrom := map[string][]externalDependency{}
	externalSummary := map[string]any{}
	for _, dep := range externals {
		label, depType := m.resolveDependencyLabelAndType(dep.Name)
		dep.Name = label
		dep.Type = depType
		externalsByFrom[dep.FromService] = append(externalsByFrom[dep.FromService], dep)
		key := m.renderEntityLabel(dep.FromService, dep.FromSidecar, "service") + " -> " + m.renderEntityLabel(dep.Name, dep.FromSidecar, dep.Type)
		key += " [" + formatDurationDisplay(dep.Duration) + "]"
		externalSummary[key] = "x" + strconv.Itoa(dep.Count)
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
		key := m.renderServiceNodeLabel(root, primarySidecarForService(root, edgesByFrom), nodeCosts[root], totalCost, 0, leaf)
		children := m.buildServiceNodeTree(root, edgesByFrom, externalsByFrom, nodeCosts, totalCost, seen)
		if len(children) == 0 {
			mapTree[key] = ""
		} else {
			mapTree[key] = children
		}
	}

	return OrderedRoot{Entries: []RootEntry{
		{Key: "nodes", Value: OrderedRoot{Entries: nodeEntries}},
		{Key: "edges", Value: edgesSummary},
		{Key: "external", Value: externalSummary},
		{Key: "map", Value: mapTree},
	}}
}

func (m Model) buildServiceNodeTree(
	service string,
	edgesByFrom map[string][]serviceEdge,
	externalsByFrom map[string][]externalDependency,
	nodeCosts map[string]time.Duration,
	totalCost time.Duration,
	seen map[string]bool,
) map[string]any {
	out := map[string]any{}

	for _, edge := range edgesByFrom[service] {
		leaf := len(edgesByFrom[edge.ToService]) == 0 && len(externalsByFrom[edge.ToService]) == 0
		callsInLabel := edge.Count
		if leaf {
			callsInLabel = 0
		}
		childKey := m.renderServiceNodeLabel(edge.ToService, edge.ToSidecar, nodeCosts[edge.ToService], totalCost, callsInLabel, leaf)
		if seen[edge.ToService] {
			out[childKey] = "(cycle)"
			continue
		}
		nextSeen := make(map[string]bool, len(seen)+1)
		for k, v := range seen {
			nextSeen[k] = v
		}
		nextSeen[edge.ToService] = true
		subtree := m.buildServiceNodeTree(edge.ToService, edgesByFrom, externalsByFrom, nodeCosts, totalCost, nextSeen)
		if len(subtree) == 0 {
			if edge.Count > 0 {
				out[childKey] = "x" + strconv.Itoa(edge.Count)
			} else {
				out[childKey] = ""
			}
		} else {
			out[childKey] = subtree
		}
	}

	for _, dep := range externalsByFrom[service] {
		leafKey := m.renderExternalNodeLabel(dep.Name, dep.Type, dep.FromSidecar, dep.Duration, totalCost, 0, true)
		out[leafKey] = "x" + strconv.Itoa(dep.Count)
	}

	return out
}

func formatCostDisplay(cost, total time.Duration, includePercent bool) string {
	if !includePercent {
		return formatDurationDisplay(cost)
	}
	if total <= 0 {
		return formatDurationDisplay(cost)
	}
	pct := (float64(cost) / float64(total)) * 100
	return fmt.Sprintf("%s (%.1f%%)", formatDurationDisplay(cost), pct)
}

func requestTotalCost(trace *domain.Trace) time.Duration {
	if trace == nil {
		return 0
	}
	type interval struct {
		start time.Time
		end   time.Time
	}
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
	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].start.Before(intervals[j].start)
	})
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

func (m Model) renderServiceNodeLabel(service, proxy string, cost, totalCost time.Duration, calls int, includePercent bool) string {
	label := m.renderEntityLabel(service, proxy, "service")
	label += " [" + m.serviceMapCostStyle().Render(formatCostDisplay(cost, totalCost, includePercent)) + "]"
	if calls > 0 {
		label += " x" + strconv.Itoa(calls)
	}
	return label
}

func (m Model) renderExternalNodeLabel(name, depType, proxy string, cost, totalCost time.Duration, calls int, includePercent bool) string {
	label := m.renderEntityLabel(name, proxy, depType)
	label += " [" + m.serviceMapCostStyle().Render(formatCostDisplay(cost, totalCost, includePercent)) + "]"
	if calls > 0 {
		label += " x" + strconv.Itoa(calls)
	}
	return label
}

func (m Model) renderEntityLabel(name, proxy, entityType string) string {
	name = strings.TrimSpace(name)
	proxy = strings.TrimSpace(proxy)
	style := m.serviceMapServiceStyle()
	if entityType != "service" {
		style = m.serviceMapDependencyStyle(entityType)
	}
	label := style.Render(name)
	if proxy != "" {
		label += " (" + m.serviceMapSidecarStyle().Render(formatProxyName(proxy)) + ")"
	}
	return label
}

func primarySidecarForService(service string, edgesByFrom map[string][]serviceEdge) string {
	counts := map[string]int{}
	for _, edge := range edgesByFrom[service] {
		if strings.TrimSpace(edge.FromSidecar) == "" {
			continue
		}
		counts[edge.FromSidecar] += edge.Count
	}
	best := ""
	bestCount := 0
	for sidecar, count := range counts {
		if count > bestCount {
			best = sidecar
			bestCount = count
		}
	}
	return best
}

func (m Model) resolveDependencyLabelAndType(name string) (string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "external", "external"
	}
	sm := m.cfg.UI.ServiceMap
	label := lookupConfigValue(sm.DependencyAliases, name)
	if label == "" {
		label = name
	}
	typeName := lookupConfigValue(sm.DependencyTypes, name)
	if typeName == "" {
		typeName = dependencyTypeFromRules(sm.DependencyTypeRules, name)
	}
	if typeName == "" {
		typeName = "external"
	}
	return label, strings.ToLower(strings.TrimSpace(typeName))
}

func lookupConfigValue(values map[string]string, key string) string {
	if len(values) == 0 {
		return ""
	}
	if v := strings.TrimSpace(values[key]); v != "" {
		return v
	}
	lower := strings.ToLower(key)
	for k, v := range values {
		if strings.EqualFold(strings.TrimSpace(k), lower) {
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func dependencyTypeFromRules(rules []config.DependencyTypeRule, name string) string {
	for _, rule := range rules {
		pattern := strings.TrimSpace(rule.Match)
		typeName := strings.TrimSpace(rule.Type)
		if pattern == "" || typeName == "" {
			continue
		}
		if matched, err := regexp.MatchString(pattern, name); err == nil && matched {
			return typeName
		}
	}
	return ""
}

func (m Model) serviceMapServiceStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(m.serviceMapColorOrDefault(m.cfg.UI.ServiceMap.ServiceColor, "68")))
}

func (m Model) serviceMapSidecarStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(m.serviceMapColorOrDefault(m.cfg.UI.ServiceMap.SidecarColor, "244")))
}

func (m Model) serviceMapDependencyStyle(typeName string) lipgloss.Style {
	typeName = strings.ToLower(strings.TrimSpace(typeName))
	if typeName != "" {
		if color := lookupConfigValue(m.cfg.UI.ServiceMap.TypeColors, typeName); color != "" {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		}
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(m.serviceMapColorOrDefault(m.cfg.UI.ServiceMap.ExternalColor, "214")))
}

func (m Model) serviceMapCostStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
}

func (m Model) serviceMapColorOrDefault(color string, fallback string) string {
	color = strings.TrimSpace(color)
	if color == "" {
		return fallback
	}
	return color
}

func (m Model) logsView(height int) string {
	var b strings.Builder
	cols := m.logColumns()
	widths := m.logColumnWidths(cols)

	b.WriteString("Logs")
	if date := m.logsHeaderDate(); date != "" {
		b.WriteString(" [")
		b.WriteString(date)
		b.WriteString("]")
	}
	b.WriteString("\n")
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
		return entry.Timestamp.In(m.loc).Format("15:04:05.000")
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
	b.WriteString("Shortcuts are config-driven (config.json). esc/? close help; tab/shift+tab move sections; gg/G top/bottom; n/N next/prev search; ctrl+f/b page; ctrl+d/u half-page; / search.")
	b.WriteString(" Press F2 (or Cmd+, when terminal supports it) for config mode.")

	return m.layout(b.String())
}

func (m *Model) openConfigMode() {
	loaded, err := config.Load(m.cfg.Path)
	if err != nil {
		m.status = "load config failed: " + err.Error()
		return
	}
	cfgModel := NewConfigModel(loaded)
	if m.width > 0 && m.height > 0 {
		updated, _ := cfgModel.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		if next, ok := updated.(ConfigModel); ok {
			cfgModel = next
		}
	}
	m.configView = &cfgModel
	m.status = "opened config mode"
}

func (m *Model) reloadConfig() {
	loaded, err := config.Load(m.cfg.Path)
	if err != nil {
		m.status = "reload config failed: " + err.Error()
		return
	}
	m.cfg = loaded
	if loc, err := loaded.DisplayLocation(); err == nil {
		m.loc = loc
	}
	m.initServiceColors()
	m.levelThresholdIx = m.levelIndex(m.cfg.Logs.LevelThreshold)
	m.applyLogThreshold()
}

func (m *Model) initServiceColors() {
	palette := defaultServicePalette()
	for _, raw := range m.cfg.UI.AdditionalServiceColors {
		color := strings.TrimSpace(raw)
		if color != "" {
			palette = append(palette, color)
		}
	}

	if len(palette) == 0 {
		palette = []string{"244"}
	}

	m.serviceColors = map[string]string{}
	next := 0
	for _, line := range m.traceLines {
		service := strings.TrimSpace(line.Service)
		if service == "" {
			continue
		}
		if _, ok := m.serviceColors[service]; ok {
			continue
		}
		m.serviceColors[service] = palette[next%len(palette)]
		next++
	}
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
		XCost:     span.XCost,
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
	FromService string
	FromSidecar string
	ToService   string
	ToSidecar   string
	Count       int
}

type edgeTarget struct {
	service     string
	fromSidecar string
	toSidecar   string
}

type externalDependency struct {
	FromService string
	FromSidecar string
	Name        string
	Type        string
	Duration    time.Duration
	Count       int
}

func buildServiceEdges(trace *domain.Trace) []serviceEdge {
	counts := map[string]int{}
	for _, span := range trace.Spans {
		if span.Service == "" || isProxySpan(span) {
			continue
		}

		targets := make([]edgeTarget, 0, len(span.Children))
		for _, child := range span.Children {
			collectProxyRoutedTargets(child, "", "", 0, &targets)
		}

		for _, target := range targets {
			if target.service == "" || target.service == span.Service {
				continue
			}
			key := strings.Join([]string{span.Service, target.fromSidecar, target.service, target.toSidecar}, "\x00")
			counts[key]++
		}
	}

	out := make([]serviceEdge, 0, len(counts))
	for key, count := range counts {
		parts := strings.Split(key, "\x00")
		out = append(out, serviceEdge{
			FromService: parts[0],
			FromSidecar: parts[1],
			ToService:   parts[2],
			ToSidecar:   parts[3],
			Count:       count,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FromService == out[j].FromService {
			if out[i].ToService == out[j].ToService {
				if out[i].FromSidecar == out[j].FromSidecar {
					return out[i].ToSidecar < out[j].ToSidecar
				}
				return out[i].FromSidecar < out[j].FromSidecar
			}
			return out[i].ToService < out[j].ToService
		}
		return out[i].FromService < out[j].FromService
	})
	return out
}

func collectProxyRoutedTargets(span *domain.Span, fromSidecar, lastProxy string, proxyDepth int, out *[]edgeTarget) {
	if span == nil {
		return
	}
	if !isProxySpan(span) {
		target := edgeTarget{
			service:     span.Service,
			fromSidecar: fromSidecar,
		}
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
		collectProxyRoutedTargets(child, fromSidecar, lastProxy, proxyDepth+1, out)
	}
}

func buildExternalDependencies(trace *domain.Trace) []externalDependency {
	type depStats struct {
		duration time.Duration
		count    int
	}

	stats := map[string]depStats{}
	for _, span := range trace.Spans {
		if span == nil || span.Service == "" || isProxySpan(span) || !isOutboundKind(span.Kind) {
			continue
		}

		targets := make([]edgeTarget, 0, len(span.Children))
		for _, child := range span.Children {
			collectProxyRoutedTargets(child, "", "", 0, &targets)
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

		depName := externalDependencyName(span)
		if depName == "" {
			continue
		}
		sidecar := firstProxyService(span.Children)
		key := strings.Join([]string{span.Service, sidecar, depName}, "\x00")
		agg := stats[key]
		agg.count++
		agg.duration += span.Duration
		stats[key] = agg
	}

	out := make([]externalDependency, 0, len(stats))
	for key, agg := range stats {
		parts := strings.Split(key, "\x00")
		out = append(out, externalDependency{
			FromService: parts[0],
			FromSidecar: parts[1],
			Name:        parts[2],
			Duration:    agg.duration,
			Count:       agg.count,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FromService == out[j].FromService {
			if out[i].Name == out[j].Name {
				return out[i].FromSidecar < out[j].FromSidecar
			}
			return out[i].Name < out[j].Name
		}
		return out[i].FromService < out[j].FromService
	})
	return out
}

func isOutboundKind(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	return strings.Contains(kind, "client") || strings.Contains(kind, "producer")
}

func firstProxyService(children []*domain.Span) string {
	for _, child := range children {
		if isProxySpan(child) {
			return strings.TrimSpace(child.Service)
		}
	}
	return ""
}

func externalDependencyName(span *domain.Span) string {
	if span == nil {
		return ""
	}
	attrs := span.Attributes
	for _, key := range []string{"peer.service", "server.address", "net.peer.name", "http.host"} {
		if v := attrString(attrs, key); v != "" {
			return v
		}
	}
	if rawURL := attrString(attrs, "http.url"); rawURL != "" {
		if parsed, err := url.Parse(rawURL); err == nil {
			if host := strings.TrimSpace(parsed.Hostname()); host != "" {
				return host
			}
		}
	}
	if system := attrString(attrs, "db.system"); system != "" {
		if name := attrString(attrs, "db.name"); name != "" {
			return system + "/" + name
		}
		return system
	}
	if system := attrString(attrs, "messaging.system"); system != "" {
		if destination := attrString(attrs, "messaging.destination.name"); destination != "" {
			return system + "/" + destination
		}
		if destination := attrString(attrs, "messaging.destination"); destination != "" {
			return system + "/" + destination
		}
		return system
	}
	if name := strings.TrimSpace(span.Name); name != "" {
		return name
	}
	return "external"
}

func attrString(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	raw, ok := attrs[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func serviceList(trace *domain.Trace) []string {
	set := map[string]struct{}{}
	for _, span := range trace.Spans {
		service := strings.TrimSpace(span.Service)
		if service == "" || isProxySpan(span) {
			continue
		}
		set[service] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func serviceNodeCosts(trace *domain.Trace) map[string]time.Duration {
	out := map[string]time.Duration{}
	for _, span := range trace.Spans {
		service := strings.TrimSpace(span.Service)
		if service == "" || isProxySpan(span) {
			continue
		}
		out[service] += span.XCost
	}
	return out
}

func serviceMapNodeCostsAll(trace *domain.Trace) map[string]time.Duration {
	out := map[string]time.Duration{}
	for _, span := range trace.Spans {
		service := strings.TrimSpace(span.Service)
		if service == "" {
			continue
		}
		if isProxySpan(span) {
			service = formatProxyName(service)
		}
		out[service] += span.XCost
	}
	return out
}

func isProxySpan(span *domain.Span) bool {
	if span == nil || span.Attributes == nil {
		return false
	}
	component, ok := span.Attributes["component"].(string)
	if !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(component), "proxy")
}

func formatProxyName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasSuffix(name, " [P]") {
		return name
	}
	return name + " [P]"
}

func (m Model) renderNodeSummaryLabel(name string, isProxy bool) string {
	if isProxy {
		return m.serviceMapSidecarStyle().Render(name)
	}
	return m.serviceMapServiceStyle().Render(name)
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
			entries = append(entries, RootEntry{Key: "events", Value: m.spanEventsPayload(span)})
		case "links":
			entries = append(entries, RootEntry{Key: "links", Value: spanLinksPayload(span.Links)})
		}
	}
	if len(entries) == 0 {
		entries = append(entries, RootEntry{Key: "metadata", Value: spanMetadataPayload(span)})
		entries = append(entries, RootEntry{Key: "attributes", Value: span.Attributes})
		entries = append(entries, RootEntry{Key: "events", Value: m.spanEventsPayload(span)})
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
		"x-cost":    span.XCost.String(),
		"status":    span.StatusCode,
		"error":     span.HasError(),
	}
}

func formatDurationDisplay(d time.Duration) string {
	return d.Round(time.Microsecond).String()
}

func (m Model) spanEventsPayload(span *domain.Span) map[string]any {
	events := map[string]any{}
	for i, event := range span.Events {
		key := fmt.Sprintf("%02d %s @ %s", i+1, event.Name, event.Time.In(m.loc).Format("15:04:05.000"))
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

	summaryBrightStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	summaryGrayStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	summarySuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	summaryInfoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	summaryWarnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	summaryErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

func sectionStyle(active bool, width int, height int) lipgloss.Style {
	color := lipgloss.Color("240")
	if active {
		color = lipgloss.Color("33")
	}
	contentWidth := max(1, width-4)
	return lipgloss.NewStyle().Width(contentWidth).Height(height).Border(lipgloss.NormalBorder()).BorderForeground(color).Padding(0, 1)
}

func (m Model) colorForService(service string) lipgloss.Style {
	service = strings.TrimSpace(service)
	if service == "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	}
	if color, ok := m.serviceColors[service]; ok {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
}

func defaultServicePalette() []string {
	return []string{"68", "173", "71", "176", "74", "179", "109", "175", "75", "181"}
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
