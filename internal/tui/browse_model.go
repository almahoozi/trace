package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/almahoozi/trace/internal/config"
	"github.com/almahoozi/trace/internal/domain"
)

type fetchSessionFunc func(context.Context, string) (*domain.Session, error)
type fetchListFunc func(context.Context) ([]domain.TraceListItem, error)

type BrowseModel struct {
	cfg          config.Config
	loc          *time.Location
	environment  string
	query        string
	items        []domain.TraceListItem
	filtered     []domain.TraceListItem
	fetchSession fetchSessionFunc
	fetchList    fetchListFunc
	openURL      func(string) error

	width        int
	height       int
	hOffset      int
	cursor       int
	loadingTrace bool
	loadingList  bool
	searchRaw    string
	search       *searchPrompt
	searchMatch  *searchMatcher
	pendingGG    bool
	status       string

	configView  *ConfigModel
	viewer      *Model
	lastSession *domain.Session
}

type browseLoadResultMsg struct {
	traceID string
	session *domain.Session
	err     error
}

type browseReloadResultMsg struct {
	items []domain.TraceListItem
	err   error
}

func NewBrowseModel(cfg config.Config, envName, query string, items []domain.TraceListItem, fetchSession fetchSessionFunc, fetchList fetchListFunc, openURL func(string) error) BrowseModel {
	status := fmt.Sprintf("env=%s traces=%d", envName, len(items))
	if strings.TrimSpace(query) != "" {
		status = fmt.Sprintf("env=%s query=%q traces=%d", envName, query, len(items))
	}
	loc := time.Local
	if cfgLoc, err := cfg.DisplayLocation(); err == nil {
		loc = cfgLoc
	}
	return BrowseModel{
		cfg:          cfg,
		loc:          loc,
		environment:  envName,
		query:        query,
		items:        items,
		filtered:     items,
		fetchSession: fetchSession,
		fetchList:    fetchList,
		openURL:      openURL,
		status:       status,
	}
}

func (m BrowseModel) LastSession() *domain.Session {
	return m.lastSession
}

func (m BrowseModel) Init() tea.Cmd {
	return nil
}

func (m BrowseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.configView != nil {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if m.isGlobalAction("quit", keyMsg.String()) {
				return m, tea.Quit
			}
			if m.isGlobalAction("back", keyMsg.String()) && m.configView.AtRoot() {
				m.configView = nil
				if loaded, err := config.Load(m.cfg.Path); err == nil {
					m.cfg = loaded
				}
				m.status = "closed config mode"
				return m, nil
			}
		}
		updated, cmd := m.configView.Update(msg)
		if next, ok := updated.(ConfigModel); ok {
			m.configView = &next
		}
		return m, cmd
	}

	if m.viewer != nil {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			key := keyMsg.String()
			if m.isGlobalAction("back", key) && m.viewerAtRoot() {
				m.viewer = nil
				m.status = "back to trace list"
				return m, nil
			}
		}
		updated, cmd := m.viewer.Update(msg)
		if next, ok := updated.(Model); ok {
			m.viewer = &next
		}
		return m, cmd
	}

	if m.search != nil {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			return m.updateSearchPrompt(keyMsg)
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.hOffset = min(m.hOffset, m.maxHorizontalOffset())
		return m, nil
	case browseLoadResultMsg:
		m.loadingTrace = false
		if msg.err != nil {
			m.status = fmt.Sprintf("failed to open %s: %v", msg.traceID, msg.err)
			return m, nil
		}
		m.status = fmt.Sprintf("loaded trace %s", msg.traceID)
		m.lastSession = msg.session
		viewer := NewModel(m.cfg, msg.session, m.openURL)
		if m.width > 0 && m.height > 0 {
			updated, _ := viewer.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			if next, ok := updated.(Model); ok {
				viewer = next
			}
		}
		m.viewer = &viewer
		return m, nil
	case browseReloadResultMsg:
		m.loadingList = false
		if msg.err != nil {
			m.status = fmt.Sprintf("reload failed: %v", msg.err)
			return m, nil
		}
		m.items = msg.items
		m.applySearchFilter()
		m.status = fmt.Sprintf("reloaded %d traces", len(m.items))
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		if m.isGlobalAction("quit", key) {
			return m, tea.Quit
		}
		if key == "/" {
			m.search = newSearchPrompt(m.searchRaw)
			m.status = "search: type query and press enter"
			return m, nil
		}
		if m.consumeGGPrefix(key) {
			return m, nil
		}
		if m.handleHorizontalScrollShortcut(key) {
			return m, nil
		}
		if m.handleTopBottomShortcut(key) {
			return m, nil
		}
		if m.handleSearchRepeatShortcut(key) {
			return m, nil
		}

		if isConfigHotkey(key) {
			cfgModel := NewConfigModel(m.cfg)
			if m.width > 0 && m.height > 0 {
				updated, _ := cfgModel.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
				if next, ok := updated.(ConfigModel); ok {
					cfgModel = next
				}
			}
			m.configView = &cfgModel
			m.status = "opened config mode"
			return m, nil
		}
		if !m.loadingList && (strings.EqualFold(key, "r") || strings.EqualFold(key, "ctrl+r")) {
			m.loadingList = true
			m.status = "reloading trace list"
			return m, m.reloadListCmd()
		}
		if m.isMoveUp(key) && m.cursor > 0 {
			m.cursor--
			return m, nil
		}
		if m.isMoveDown(key) && m.cursor < len(m.filtered)-1 {
			m.cursor++
			return m, nil
		}
		if m.isPageDown(key) && len(m.filtered) > 0 {
			m.cursor = min(len(m.filtered)-1, m.cursor+m.listRows())
			return m, nil
		}
		if m.isPageUp(key) && len(m.filtered) > 0 {
			m.cursor = max(0, m.cursor-m.listRows())
			return m, nil
		}
		if m.isHalfPageDown(key) && len(m.filtered) > 0 {
			m.cursor = min(len(m.filtered)-1, m.cursor+m.halfPageRows())
			return m, nil
		}
		if m.isHalfPageUp(key) && len(m.filtered) > 0 {
			m.cursor = max(0, m.cursor-m.halfPageRows())
			return m, nil
		}
		if !m.loadingTrace && !m.loadingList && strings.EqualFold(key, "enter") {
			item, ok := m.current()
			if !ok {
				return m, nil
			}
			m.loadingTrace = true
			m.status = "loading trace " + item.TraceID
			return m, m.loadSessionCmd(item.TraceID)
		}
	}

	return m, nil
}

func (m BrowseModel) View() string {
	if m.viewer != nil {
		return m.viewer.View()
	}
	if m.configView != nil {
		return m.configView.View()
	}
	if m.width == 0 || m.height == 0 {
		return "loading..."
	}

	startWidth := 23
	traceIDWidth := 32
	svcWidth := 16
	statsWidth := 12
	durationWidth := 10
	remaining := max(24, m.width-(2+startWidth+traceIDWidth+svcWidth+statsWidth+durationWidth+15))
	opWidth := remaining

	head := fmt.Sprintf("%-*s | %-*s | %-*s | %-*s | %-*s | %-*s",
		startWidth, "start time",
		traceIDWidth, "trace id",
		opWidth, "operation",
		svcWidth, "service",
		statsWidth, "nErrors/nSpans",
		durationWidth, "duration",
	)

	var b strings.Builder
	b.WriteString(titleStyle.Render("trace browse mode"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("env=%s", m.environment)))
	if strings.TrimSpace(m.query) != "" {
		b.WriteString(mutedStyle.Render(fmt.Sprintf(" query=%q", m.query)))
	}
	b.WriteString("\n\n")
	b.WriteString(sliceHorizontal(head, m.hOffset, m.width))
	b.WriteString("\n")
	separatorWidth := max(0, len(head)-m.hOffset)
	b.WriteString(strings.Repeat("-", min(max(0, m.width), separatorWidth)))
	b.WriteString("\n")

	rows := m.listRows()
	start, end := browseWindow(len(m.filtered), m.cursor, rows)
	for i := start; i < end; i++ {
		item := m.filtered[i]
		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}
		line := fmt.Sprintf("%s%-*s | %-*s | %-*s | %-*s | %-*s | %-*s",
			prefix,
			startWidth, truncate(formatBrowseStartTime(item.StartTime, m.loc), startWidth),
			traceIDWidth, truncate(item.TraceID, traceIDWidth),
			opWidth, truncate(defaultDash(item.OperationName), opWidth),
			svcWidth, truncate(defaultDash(item.Service), svcWidth),
			statsWidth, padRight(fmt.Sprintf("%d/%d", item.ErrorSpanCount, item.SpanCount), statsWidth),
			durationWidth, padRight(formatBrowseDuration(item.Duration), durationWidth),
		)
		b.WriteString(sliceHorizontal(line, m.hOffset, m.width))
		b.WriteString("\n")
	}

	if len(m.filtered) == 0 {
		b.WriteString("(no traces found)\n")
	}

	footer := m.status + " | / search | n/N next/prev | gg/G top/bottom | <-/-> or h/l scroll | enter open | F2 config | esc back from trace | ctrl+f/b page | ctrl+d/u half page | r reload | q quit"
	if m.loadingTrace {
		footer = "loading trace..."
	}
	if m.loadingList {
		footer = "reloading trace list..."
	}
	if m.search != nil {
		footer += "\n" + m.search.viewLine() + "\n" + searchHint()
	}
	b.WriteString(mutedStyle.Render(footer))

	return clampToHeight(b.String(), m.height)
}

func (m BrowseModel) isGlobalAction(action, pressed string) bool {
	for _, key := range m.cfg.Keymap.Global[action] {
		if keysMatch(key, pressed) {
			return true
		}
	}
	return false
}

func (m BrowseModel) isMoveUp(pressed string) bool {
	if keysMatch("k", pressed) || keysMatch("up", pressed) {
		return true
	}
	for _, key := range m.cfg.Keymap.Trace["up"] {
		if keysMatch(key, pressed) {
			return true
		}
	}
	return false
}

func (m BrowseModel) isMoveDown(pressed string) bool {
	if keysMatch("j", pressed) || keysMatch("down", pressed) {
		return true
	}
	for _, key := range m.cfg.Keymap.Trace["down"] {
		if keysMatch(key, pressed) {
			return true
		}
	}
	return false
}

func (m BrowseModel) isPageDown(pressed string) bool {
	return keysMatch("pgdown", pressed) || keysMatch("ctrl+f", pressed)
}

func (m BrowseModel) isPageUp(pressed string) bool {
	return keysMatch("pgup", pressed) || keysMatch("ctrl+b", pressed)
}

func (m BrowseModel) isHalfPageDown(pressed string) bool {
	return keysMatch("ctrl+d", pressed)
}

func (m BrowseModel) isHalfPageUp(pressed string) bool {
	return keysMatch("ctrl+u", pressed)
}

func (m *BrowseModel) consumeGGPrefix(key string) bool {
	if m.pendingGG {
		m.pendingGG = false
		if key == "g" {
			m.cursor = 0
			return true
		}
	}
	if key == "g" {
		m.pendingGG = true
		return true
	}
	return false
}

func (m *BrowseModel) handleTopBottomShortcut(key string) bool {
	if key != "G" && key != "shift+g" {
		return false
	}
	if len(m.filtered) > 0 {
		m.cursor = len(m.filtered) - 1
	}
	return true
}

func (m *BrowseModel) handleHorizontalScrollShortcut(key string) bool {
	maxOffset := m.maxHorizontalOffset()
	if maxOffset <= 0 {
		m.hOffset = 0
		return false
	}

	step := 8
	switch key {
	case "left", "h":
		m.hOffset = max(0, m.hOffset-step)
		return true
	case "right", "l":
		m.hOffset = min(maxOffset, m.hOffset+step)
		return true
	case "home":
		m.hOffset = 0
		return true
	case "end":
		m.hOffset = maxOffset
		return true
	default:
		return false
	}
}

func (m *BrowseModel) handleSearchRepeatShortcut(key string) bool {
	if m.searchMatch == nil {
		return false
	}
	if key == "n" {
		if len(m.filtered) > 0 {
			m.cursor = (m.cursor + 1) % len(m.filtered)
		}
		return true
	}
	if key == "N" || key == "shift+n" {
		if len(m.filtered) > 0 {
			m.cursor = (m.cursor - 1 + len(m.filtered)) % len(m.filtered)
		}
		return true
	}
	return false
}

func (m BrowseModel) updateSearchPrompt(keyMsg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.search == nil {
		return m, nil
	}
	switch keyMsg.String() {
	case "esc":
		m.search = nil
		m.status = "search cancelled"
		return m, nil
	case "enter":
		raw := strings.TrimSpace(m.search.value())
		m.search = nil
		matcher, err := compileSearchMatcher(raw)
		if err != nil {
			m.status = "search parse failed: " + err.Error()
			return m, nil
		}
		m.searchRaw = raw
		m.searchMatch = matcher
		m.applySearchFilter()
		if matcher == nil {
			m.status = fmt.Sprintf("search cleared (%d traces)", len(m.filtered))
		} else {
			m.status = fmt.Sprintf("search %q matched %d traces", raw, len(m.filtered))
		}
		return m, nil
	}
	if m.search.applyKey(keyMsg) {
		return m, nil
	}
	return m, nil
}

func (m *BrowseModel) applySearchFilter() {
	if m.searchMatch == nil {
		m.filtered = m.items
	} else {
		filtered := make([]domain.TraceListItem, 0, len(m.items))
		for _, item := range m.items {
			fields := map[string]string{
				"trace.id":   item.TraceID,
				"operation":  item.OperationName,
				"service":    item.Service,
				"errors":     fmt.Sprintf("%d", item.ErrorSpanCount),
				"spans":      fmt.Sprintf("%d", item.SpanCount),
				"duration":   item.Duration.String(),
				"start_time": item.StartTime.Format(time.RFC3339Nano),
			}
			blob := strings.Join([]string{item.TraceID, item.OperationName, item.Service, item.Duration.String()}, " ")
			if m.searchMatch.MatchFields(fields, blob) {
				filtered = append(filtered, item)
			}
		}
		m.filtered = filtered
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

func (m BrowseModel) current() (domain.TraceListItem, bool) {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return domain.TraceListItem{}, false
	}
	return m.filtered[m.cursor], true
}

func (m BrowseModel) listRows() int {
	return max(1, m.height-7)
}

func (m BrowseModel) halfPageRows() int {
	return max(1, m.listRows()/2)
}

func (m BrowseModel) maxHorizontalOffset() int {
	startWidth := 23
	traceIDWidth := 32
	svcWidth := 16
	statsWidth := 12
	durationWidth := 10
	remaining := max(24, m.width-(2+startWidth+traceIDWidth+svcWidth+statsWidth+durationWidth+15))
	tableWidth := 2 + startWidth + 3 + traceIDWidth + 3 + remaining + 3 + svcWidth + 3 + statsWidth + 3 + durationWidth
	return max(0, tableWidth-m.width)
}

func (m BrowseModel) viewerAtRoot() bool {
	if m.viewer == nil {
		return true
	}
	return !m.viewer.showHelp && m.viewer.jsonTree == nil && m.viewer.valueView == nil && m.viewer.searchPrompt == nil
}

func (m BrowseModel) loadSessionCmd(traceID string) tea.Cmd {
	return func() tea.Msg {
		if m.fetchSession == nil {
			return browseLoadResultMsg{traceID: traceID, err: fmt.Errorf("trace fetcher not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.cfg.Grafana.TimeoutSeconds+10)*time.Second)
		defer cancel()
		session, err := m.fetchSession(ctx, traceID)
		return browseLoadResultMsg{traceID: traceID, session: session, err: err}
	}
}

func (m BrowseModel) reloadListCmd() tea.Cmd {
	return func() tea.Msg {
		if m.fetchList == nil {
			return browseReloadResultMsg{err: fmt.Errorf("trace list fetcher not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.cfg.Grafana.TimeoutSeconds+10)*time.Second)
		defer cancel()
		items, err := m.fetchList(ctx)
		return browseReloadResultMsg{items: items, err: err}
	}
}

func formatBrowseDuration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	if d < time.Millisecond {
		return d.Round(time.Microsecond).String()
	}
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(10 * time.Millisecond).String()
}

func formatBrowseStartTime(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return "-"
	}
	if loc == nil {
		loc = time.Local
	}
	return t.In(loc).Format("2006-01-02 15:04:05.000")
}

func sliceHorizontal(line string, offset, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(line)
	if offset <= 0 {
		offset = 0
	}
	if offset >= len(runes) {
		return ""
	}
	end := min(len(runes), offset+width)
	return string(runes[offset:end])
}

func browseWindow(total, cursor, visible int) (int, int) {
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

func defaultDash(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "-"
	}
	return v
}
