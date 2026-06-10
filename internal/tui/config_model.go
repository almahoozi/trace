package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/almahoozi/trace/internal/config"
	"github.com/almahoozi/trace/internal/platform"
)

type configFieldKind int

const (
	configFieldScalar configFieldKind = iota
	configFieldJSON
)

type configField struct {
	Path       string
	Label      string
	Value      any
	Default    any
	HasDefault bool
	Kind       configFieldKind
}

type configEditState struct {
	path       string
	kind       configFieldKind
	original   any
	input      []rune
	cursor     int
	hasDefault bool
	defaultVal any
}

type ConfigModel struct {
	cfg         config.Config
	currentRoot map[string]any
	defaultRoot map[string]any

	width  int
	height int

	sectionCursor int
	fieldCursor   int
	section       string

	sections []string
	fields   []configField

	editing *configEditState
	status  string
}

type configOpenEditorResultMsg struct {
	err error
}

func NewConfigModel(cfg config.Config) ConfigModel {
	currentRoot := configToRootMap(cfg)
	defaultRoot := configToRootMap(config.DefaultConfig())
	sections := rootSectionNames(currentRoot)
	return ConfigModel{
		cfg:         cfg,
		currentRoot: currentRoot,
		defaultRoot: defaultRoot,
		sections:    sections,
		status:      fmt.Sprintf("config=%s", cfg.Path),
	}
}

func (m ConfigModel) Init() tea.Cmd {
	return nil
}

func (m ConfigModel) AtRoot() bool {
	return m.section == "" && m.editing == nil
}

func (m ConfigModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case configOpenEditorResultMsg:
		if msg.err != nil {
			m.status = "open editor failed: " + msg.err.Error()
		} else {
			m.status = "opened config file in editor"
		}
		return m, nil
	case tea.KeyMsg:
		key := msg.String()

		if m.isQuit(key) {
			return m, tea.Quit
		}
		if strings.EqualFold(key, "o") {
			return m, m.openEditorCmd()
		}

		if m.editing != nil {
			return m.updateEditing(msg)
		}

		if m.section == "" {
			return m.updateSections(key)
		}
		return m.updateSectionFields(key)
	}
	return m, nil
}

func (m ConfigModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading..."
	}

	if m.section == "" {
		return clampToHeight(m.viewSections(), m.height)
	}
	if m.editing != nil {
		return clampToHeight(m.viewEditor(), m.height)
	}
	return clampToHeight(m.viewSectionFields(), m.height)
}

func (m ConfigModel) updateSections(key string) (tea.Model, tea.Cmd) {
	if len(m.sections) == 0 {
		m.status = "no configurable sections found"
		return m, nil
	}

	if m.isMoveUp(key) && m.sectionCursor > 0 {
		m.sectionCursor--
		return m, nil
	}
	if m.isMoveDown(key) && m.sectionCursor < len(m.sections)-1 {
		m.sectionCursor++
		return m, nil
	}

	if strings.EqualFold(key, "enter") || strings.EqualFold(key, "e") {
		m.section = m.sections[m.sectionCursor]
		m.fieldCursor = 0
		m.rebuildFields()
		m.status = fmt.Sprintf("editing section %s", m.section)
		return m, nil
	}

	if strings.EqualFold(key, "r") {
		name := m.sections[m.sectionCursor]
		if err := m.resetPath(name); err != nil {
			m.status = "reset failed: " + err.Error()
		} else {
			m.status = fmt.Sprintf("reset %s to defaults", name)
		}
		return m, nil
	}

	return m, nil
}

func (m ConfigModel) updateSectionFields(key string) (tea.Model, tea.Cmd) {
	if m.isBack(key) {
		m.section = ""
		m.fields = nil
		m.fieldCursor = 0
		m.status = "back to sections"
		return m, nil
	}

	if len(m.fields) == 0 {
		if strings.EqualFold(key, "r") {
			if err := m.resetPath(m.section); err != nil {
				m.status = "reset failed: " + err.Error()
			} else {
				m.status = fmt.Sprintf("reset %s to defaults", m.section)
			}
		}
		return m, nil
	}

	if m.isMoveUp(key) && m.fieldCursor > 0 {
		m.fieldCursor--
		return m, nil
	}
	if m.isMoveDown(key) && m.fieldCursor < len(m.fields)-1 {
		m.fieldCursor++
		return m, nil
	}

	if strings.EqualFold(key, "r") {
		field := m.fields[m.fieldCursor]
		if err := m.resetPath(field.Path); err != nil {
			m.status = "reset failed: " + err.Error()
		} else {
			m.status = fmt.Sprintf("reset %s", field.Path)
			m.rebuildFields()
		}
		return m, nil
	}

	if strings.EqualFold(key, "ctrl+r") {
		if err := m.resetPath(m.section); err != nil {
			m.status = "reset failed: " + err.Error()
		} else {
			m.status = fmt.Sprintf("reset %s to defaults", m.section)
			m.rebuildFields()
		}
		return m, nil
	}

	if strings.EqualFold(key, "enter") || strings.EqualFold(key, "e") {
		field := m.fields[m.fieldCursor]
		m.editing = &configEditState{
			path:       field.Path,
			kind:       field.Kind,
			original:   cloneAny(field.Value),
			hasDefault: field.HasDefault,
			defaultVal: cloneAny(field.Default),
		}
		initial := initialEditText(field)
		m.editing.input = []rune(initial)
		m.editing.cursor = len(m.editing.input)
		return m, nil
	}

	return m, nil
}

func (m ConfigModel) updateEditing(keyMsg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.editing == nil {
		return m, nil
	}

	key := keyMsg.String()
	switch key {
	case "esc":
		m.editing = nil
		m.status = "edit cancelled"
		return m, nil
	case "enter":
		savedPath := m.editing.path
		next, err := parseEditedValue(m.editing.kind, m.editing.original, string(m.editing.input))
		if err != nil {
			m.status = "invalid value: " + err.Error()
			return m, nil
		}
		if err := m.setPathValue(m.editing.path, next); err != nil {
			m.status = "save failed: " + err.Error()
			return m, nil
		}
		m.editing = nil
		m.rebuildFields()
		m.status = "saved " + savedPath
		return m, nil
	case "left":
		if m.editing.cursor > 0 {
			m.editing.cursor--
		}
		return m, nil
	case "right":
		if m.editing.cursor < len(m.editing.input) {
			m.editing.cursor++
		}
		return m, nil
	case "home":
		m.editing.cursor = 0
		return m, nil
	case "end":
		m.editing.cursor = len(m.editing.input)
		return m, nil
	case "backspace":
		if m.editing.cursor > 0 {
			m.editing.input = append(m.editing.input[:m.editing.cursor-1], m.editing.input[m.editing.cursor:]...)
			m.editing.cursor--
		}
		return m, nil
	case "delete":
		if m.editing.cursor < len(m.editing.input) {
			m.editing.input = append(m.editing.input[:m.editing.cursor], m.editing.input[m.editing.cursor+1:]...)
		}
		return m, nil
	case "ctrl+r":
		if !m.editing.hasDefault {
			m.status = "no default available for this field"
			return m, nil
		}
		if err := m.setPathValue(m.editing.path, cloneAny(m.editing.defaultVal)); err != nil {
			m.status = "reset failed: " + err.Error()
			return m, nil
		}
		m.editing = nil
		m.rebuildFields()
		m.status = "field reset to default"
		return m, nil
	}

	if keyMsg.Type == tea.KeyRunes {
		insert := keyMsg.Runes
		if len(insert) > 0 {
			left := append([]rune{}, m.editing.input[:m.editing.cursor]...)
			right := append([]rune{}, m.editing.input[m.editing.cursor:]...)
			left = append(left, insert...)
			left = append(left, right...)
			m.editing.input = left
			m.editing.cursor += len(insert)
		}
	}

	return m, nil
}

func (m ConfigModel) viewSections() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("trace config mode"))
	b.WriteString("\n")
	b.WriteString("Sections\n")
	b.WriteString(strings.Repeat("-", max(20, min(80, m.width-2))))
	b.WriteString("\n")

	if len(m.sections) == 0 {
		b.WriteString("(no sections)\n")
	} else {
		start, end := browseWindow(len(m.sections), m.sectionCursor, max(1, m.height-7))
		for i := start; i < end; i++ {
			prefix := "  "
			if i == m.sectionCursor {
				prefix = "> "
			}
			section := m.sections[i]
			value, _ := getByPath(m.currentRoot, section)
			b.WriteString(prefix)
			b.WriteString(padRight(section, 20))
			b.WriteString(" ")
			b.WriteString(truncate(previewValue(value), max(10, m.width-28)))
			b.WriteString("\n")
		}
	}

	footer := m.status + " | enter/e open section | r reset section | o open config | q quit"
	b.WriteString(mutedStyle.Render(footer))
	return b.String()
}

func (m ConfigModel) viewSectionFields() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("trace config mode"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Section: %s\n", m.section))
	b.WriteString(strings.Repeat("-", max(20, min(80, m.width-2))))
	b.WriteString("\n")

	if len(m.fields) == 0 {
		b.WriteString("(no editable fields)\n")
	} else {
		rows := max(1, m.height-8)
		start, end := browseWindow(len(m.fields), m.fieldCursor, rows)
		for i := start; i < end; i++ {
			field := m.fields[i]
			prefix := "  "
			if i == m.fieldCursor {
				prefix = "> "
			}
			kind := "value"
			if field.Kind == configFieldJSON {
				kind = "json "
			}
			labelWidth := max(18, min(42, m.width/3))
			line := fmt.Sprintf("%s%s %-*s %s", prefix, kind, labelWidth, field.Label, truncate(previewValue(field.Value), max(12, m.width-labelWidth-14)))
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	footer := m.status + " | enter/e edit | r reset field | ctrl+r reset section | o open config | esc back | q quit"
	b.WriteString(mutedStyle.Render(footer))
	return b.String()
}

func (m ConfigModel) viewEditor() string {
	edit := m.editing
	if edit == nil {
		return m.viewSectionFields()
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("trace config mode"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Edit: %s\n", edit.path))
	b.WriteString(strings.Repeat("-", max(20, min(80, m.width-2))))
	b.WriteString("\n")
	b.WriteString("Current value:\n")
	b.WriteString(truncate(previewValue(edit.original), max(20, m.width-2)))
	b.WriteString("\n\n")
	b.WriteString("Input:\n")
	line := string(edit.input)
	if edit.cursor >= 0 && edit.cursor <= len(edit.input) {
		r := []rune(line)
		if edit.cursor == len(r) {
			line = string(r) + "|"
		} else {
			line = string(r[:edit.cursor]) + "|" + string(r[edit.cursor:])
		}
	}
	b.WriteString(line)
	b.WriteString("\n")
	if edit.kind == configFieldJSON {
		b.WriteString(mutedStyle.Render("JSON editor: use compact JSON (single line) for objects/arrays."))
		b.WriteString("\n")
	}
	footer := "enter save | esc cancel | ctrl+r reset default | o open config"
	b.WriteString(mutedStyle.Render(footer))
	return b.String()
}

func (m ConfigModel) openEditorCmd() tea.Cmd {
	return func() tea.Msg {
		path := strings.TrimSpace(m.cfg.Path)
		if path == "" {
			var err error
			path, err = config.EnsureFile("")
			if err != nil {
				return configOpenEditorResultMsg{err: err}
			}
		}
		return configOpenEditorResultMsg{err: platform.OpenInEditor(path)}
	}
}

func (m *ConfigModel) rebuildFields() {
	if m.section == "" {
		m.fields = nil
		return
	}
	value, ok := getByPath(m.currentRoot, m.section)
	if !ok {
		m.fields = nil
		return
	}
	defaultValue, hasDefault := getByPath(m.defaultRoot, m.section)
	if !hasDefault {
		defaultValue = nil
	}
	fields := make([]configField, 0, 64)
	collectSectionFields(m.section, "", value, defaultValue, hasDefault, &fields)
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Path < fields[j].Path
	})
	m.fields = fields
	if m.fieldCursor >= len(m.fields) {
		m.fieldCursor = max(0, len(m.fields)-1)
	}
}

func collectSectionFields(path, label string, value any, defaultValue any, hasDefault bool, fields *[]configField) {
	if label == "" {
		label = "(section)"
	}

	switch typed := value.(type) {
	case map[string]any:
		*fields = append(*fields, configField{Path: path, Label: label, Value: cloneAny(value), Default: cloneAny(defaultValue), HasDefault: hasDefault, Kind: configFieldJSON})
		keys := make([]string, 0, len(typed))
		for k := range typed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childPath := path + "." + key
			childLabel := key
			if label != "(section)" {
				childLabel = label + "." + key
			}
			childDefault, childHasDefault := mapDefaultChild(defaultValue, key, hasDefault)
			collectSectionFields(childPath, childLabel, typed[key], childDefault, childHasDefault, fields)
		}
	case []any:
		*fields = append(*fields, configField{Path: path, Label: label, Value: cloneAny(value), Default: cloneAny(defaultValue), HasDefault: hasDefault, Kind: configFieldJSON})
		for i, child := range typed {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			childLabel := fmt.Sprintf("%s[%d]", label, i)
			childDefault, childHasDefault := sliceDefaultChild(defaultValue, i, hasDefault)
			collectSectionFields(childPath, childLabel, child, childDefault, childHasDefault, fields)
		}
	default:
		*fields = append(*fields, configField{Path: path, Label: label, Value: cloneAny(value), Default: cloneAny(defaultValue), HasDefault: hasDefault, Kind: configFieldScalar})
	}
}

func mapDefaultChild(v any, key string, hasDefault bool) (any, bool) {
	if !hasDefault {
		return nil, false
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	child, ok := m[key]
	return cloneAny(child), ok
}

func sliceDefaultChild(v any, idx int, hasDefault bool) (any, bool) {
	if !hasDefault {
		return nil, false
	}
	slice, ok := v.([]any)
	if !ok || idx < 0 || idx >= len(slice) {
		return nil, false
	}
	return cloneAny(slice[idx]), true
}

func initialEditText(field configField) string {
	if field.Kind == configFieldJSON {
		buf, err := json.Marshal(field.Value)
		if err != nil {
			return ""
		}
		return string(buf)
	}
	switch typed := field.Value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func parseEditedValue(kind configFieldKind, original any, raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	if kind == configFieldJSON {
		if raw == "" {
			return nil, nil
		}
		var out any
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return nil, err
		}
		return out, nil
	}

	switch original.(type) {
	case string:
		return raw, nil
	case bool:
		v, err := strconv.ParseBool(strings.ToLower(raw))
		if err != nil {
			return nil, err
		}
		return v, nil
	case float64:
		if raw == "" {
			return float64(0), nil
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, err
		}
		return v, nil
	case nil:
		if raw == "" {
			return nil, nil
		}
		if strings.EqualFold(raw, "null") {
			return nil, nil
		}
		if strings.EqualFold(raw, "true") || strings.EqualFold(raw, "false") {
			v, _ := strconv.ParseBool(strings.ToLower(raw))
			return v, nil
		}
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			return v, nil
		}
		return raw, nil
	default:
		if raw == "" {
			return nil, nil
		}
		return raw, nil
	}
}

func (m *ConfigModel) resetPath(path string) error {
	value, ok := getByPath(m.defaultRoot, path)
	if !ok {
		current, hasCurrent := getByPath(m.currentRoot, path)
		if !hasCurrent {
			return fmt.Errorf("path not found")
		}
		value = zeroLike(current)
	}
	return m.setPathValue(path, value)
}

func (m *ConfigModel) setPathValue(path string, value any) error {
	before := cloneRootMap(m.currentRoot)
	next, err := setByPath(m.currentRoot, path, cloneAny(value))
	if err != nil {
		return err
	}
	decoded, err := rootMapToConfig(next, m.cfg)
	if err != nil {
		m.currentRoot = before
		return err
	}
	if err := config.Save(decoded); err != nil {
		m.currentRoot = before
		return err
	}
	m.cfg = decoded
	m.currentRoot = next
	m.sections = rootSectionNames(m.currentRoot)
	if m.sectionCursor >= len(m.sections) {
		m.sectionCursor = max(0, len(m.sections)-1)
	}
	return nil
}

func (m ConfigModel) isQuit(key string) bool {
	for _, candidate := range m.cfg.Keymap.Global["quit"] {
		if keysMatch(candidate, key) {
			return true
		}
	}
	return strings.EqualFold(key, "q") || strings.EqualFold(key, "ctrl+c")
}

func (m ConfigModel) isBack(key string) bool {
	for _, candidate := range m.cfg.Keymap.Global["back"] {
		if keysMatch(candidate, key) {
			return true
		}
	}
	return strings.EqualFold(key, "esc")
}

func (m ConfigModel) isMoveUp(key string) bool {
	if strings.EqualFold(key, "up") || strings.EqualFold(key, "k") {
		return true
	}
	for _, candidate := range m.cfg.Keymap.Trace["up"] {
		if keysMatch(candidate, key) {
			return true
		}
	}
	return false
}

func (m ConfigModel) isMoveDown(key string) bool {
	if strings.EqualFold(key, "down") || strings.EqualFold(key, "j") {
		return true
	}
	for _, candidate := range m.cfg.Keymap.Trace["down"] {
		if keysMatch(candidate, key) {
			return true
		}
	}
	return false
}

func configToRootMap(cfg config.Config) map[string]any {
	buf, _ := json.Marshal(cfg)
	root := map[string]any{}
	_ = json.Unmarshal(buf, &root)
	return root
}

func rootMapToConfig(root map[string]any, original config.Config) (config.Config, error) {
	buf, err := json.Marshal(root)
	if err != nil {
		return config.Config{}, err
	}
	next := config.DefaultConfig()
	if err := json.Unmarshal(buf, &next); err != nil {
		return config.Config{}, err
	}
	next.Path = original.Path
	next.ConfigDir = original.ConfigDir
	return next, nil
}

func cloneRootMap(in map[string]any) map[string]any {
	out, ok := cloneAny(in).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return out
}

func cloneAny(v any) any {
	buf, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(buf, &out); err != nil {
		return v
	}
	return out
}

func previewValue(v any) string {
	switch typed := v.(type) {
	case string:
		return typed
	case map[string]any, []any:
		buf, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(buf)
	case nil:
		return "null"
	default:
		return fmt.Sprint(v)
	}
}

func zeroLike(v any) any {
	switch v.(type) {
	case string:
		return ""
	case bool:
		return false
	case float64:
		return float64(0)
	case []any:
		return []any{}
	case map[string]any:
		return map[string]any{}
	default:
		return nil
	}
}

func rootSectionNames(root map[string]any) []string {
	keys := make([]string, 0, len(root))
	for key := range root {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type pathToken struct {
	key   string
	index int
	kind  string
}

func parsePath(path string) ([]pathToken, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	tokens := make([]pathToken, 0, 8)
	i := 0
	for i < len(path) {
		switch path[i] {
		case '.':
			i++
		case '[':
			end := strings.IndexByte(path[i:], ']')
			if end <= 1 {
				return nil, fmt.Errorf("invalid index segment")
			}
			end += i
			number := path[i+1 : end]
			idx, err := strconv.Atoi(number)
			if err != nil || idx < 0 {
				return nil, fmt.Errorf("invalid index %q", number)
			}
			tokens = append(tokens, pathToken{kind: "index", index: idx})
			i = end + 1
		default:
			start := i
			for i < len(path) && path[i] != '.' && path[i] != '[' {
				i++
			}
			key := strings.TrimSpace(path[start:i])
			if key == "" {
				return nil, fmt.Errorf("invalid path segment")
			}
			tokens = append(tokens, pathToken{kind: "key", key: key})
		}
	}
	return tokens, nil
}

func getByPath(root map[string]any, path string) (any, bool) {
	tokens, err := parsePath(path)
	if err != nil {
		return nil, false
	}
	cur := any(root)
	for _, token := range tokens {
		switch token.kind {
		case "key":
			obj, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			next, ok := obj[token.key]
			if !ok {
				return nil, false
			}
			cur = next
		case "index":
			arr, ok := cur.([]any)
			if !ok || token.index < 0 || token.index >= len(arr) {
				return nil, false
			}
			cur = arr[token.index]
		}
	}
	return cloneAny(cur), true
}

func setByPath(root map[string]any, path string, value any) (map[string]any, error) {
	tokens, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	updated, err := setByTokens(any(cloneRootMap(root)), tokens, value)
	if err != nil {
		return nil, err
	}
	out, ok := updated.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid root value")
	}
	return out, nil
}

func setByTokens(current any, tokens []pathToken, value any) (any, error) {
	if len(tokens) == 0 {
		return cloneAny(value), nil
	}
	first := tokens[0]
	nextTokens := tokens[1:]

	switch first.kind {
	case "key":
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("path expects object at %s", first.key)
		}
		child, exists := obj[first.key]
		if !exists {
			child = containerForTokens(nextTokens)
		}
		updatedChild, err := setByTokens(child, nextTokens, value)
		if err != nil {
			return nil, err
		}
		obj[first.key] = updatedChild
		return obj, nil
	case "index":
		arr, ok := current.([]any)
		if !ok {
			return nil, fmt.Errorf("path expects array at index %d", first.index)
		}
		for len(arr) <= first.index {
			arr = append(arr, nil)
		}
		child := arr[first.index]
		if child == nil {
			child = containerForTokens(nextTokens)
		}
		updatedChild, err := setByTokens(child, nextTokens, value)
		if err != nil {
			return nil, err
		}
		arr[first.index] = updatedChild
		return arr, nil
	default:
		return nil, fmt.Errorf("invalid token")
	}
}

func containerForTokens(tokens []pathToken) any {
	if len(tokens) == 0 {
		return nil
	}
	if tokens[0].kind == "index" {
		return []any{}
	}
	return map[string]any{}
}
