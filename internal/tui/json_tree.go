package tui

import (
	"fmt"
	"sort"
	"strings"
)

type jsonLine struct {
	Path        string
	Depth       int
	Key         string
	Label       string
	Value       any
	Collapsable bool
	Expanded    bool
}

type JSONTree struct {
	title     string
	root      any
	expanded  map[string]bool
	lines     []jsonLine
	cursor    int
	scrollTop int
	viewRows  int
	visual    bool
	anchor    int
}

type RootEntry struct {
	Key   string
	Value any
}

type OrderedRoot struct {
	Entries []RootEntry
}

func NewJSONTree(title string, root any) *JSONTree {
	return NewJSONTreeWithExpanded(title, root, "$")
}

func NewJSONTreeExpandedAll(title string, root any) *JSONTree {
	expanded := map[string]bool{}
	collectExpandablePaths("$", root, expanded)
	if len(expanded) == 0 {
		expanded["$"] = true
	}
	j := &JSONTree{title: title, root: root, expanded: expanded}
	j.rebuild()
	return j
}

func NewJSONTreeWithExpanded(title string, root any, expandedPaths ...string) *JSONTree {
	expanded := map[string]bool{}
	for _, path := range expandedPaths {
		expanded[path] = true
	}
	if len(expanded) == 0 {
		expanded["$"] = true
	}
	j := &JSONTree{
		title:    title,
		root:     root,
		expanded: expanded,
	}
	j.rebuild()
	return j
}

func (j *JSONTree) MoveUp() {
	if j.cursor > 0 {
		j.cursor--
	}
	j.ensureCursorVisible(-1, j.visibleRows())
}

func (j *JSONTree) MoveDown() {
	if j.cursor < len(j.lines)-1 {
		j.cursor++
	}
	j.ensureCursorVisible(1, j.visibleRows())
}

func (j *JSONTree) Expand() {
	if len(j.lines) == 0 {
		return
	}
	line := j.lines[j.cursor]
	if line.Collapsable {
		j.expanded[line.Path] = true
		j.rebuild()
	}
	j.ensureCursorVisible(0, j.visibleRows())
}

func (j *JSONTree) Collapse() {
	if len(j.lines) == 0 {
		return
	}
	line := j.lines[j.cursor]
	if line.Collapsable && line.Expanded {
		j.expanded[line.Path] = false
		j.rebuild()
		j.ensureCursorVisible(0, j.visibleRows())
		return
	}
	if line.Path != "$" {
		parent := parentPath(line.Path)
		for i := range j.lines {
			if j.lines[i].Path == parent {
				j.cursor = i
				j.ensureCursorVisible(-1, j.visibleRows())
				return
			}
		}
	}
	j.ensureCursorVisible(0, j.visibleRows())
}

func (j *JSONTree) Toggle() {
	if len(j.lines) == 0 {
		return
	}
	line := j.lines[j.cursor]
	if !line.Collapsable {
		return
	}
	if line.Expanded {
		j.expanded[line.Path] = false
	} else {
		j.expanded[line.Path] = true
	}
	j.rebuild()
	j.ensureCursorVisible(0, j.visibleRows())
}

func (j *JSONTree) ToggleVisualMode() bool {
	if len(j.lines) == 0 {
		j.visual = false
		j.anchor = 0
		return false
	}
	if j.visual {
		j.visual = false
		j.anchor = 0
		return false
	}
	j.visual = true
	j.anchor = max(0, min(j.cursor, len(j.lines)-1))
	return true
}

func (j *JSONTree) DisableVisualMode() bool {
	if !j.visual {
		return false
	}
	j.visual = false
	j.anchor = 0
	return true
}

func (j *JSONTree) selectionRange() (int, int) {
	if len(j.lines) == 0 {
		return 0, 0
	}
	cursor := max(0, min(j.cursor, len(j.lines)-1))
	if !j.visual {
		return cursor, cursor
	}
	anchor := max(0, min(j.anchor, len(j.lines)-1))
	return min(anchor, cursor), max(anchor, cursor)
}

func (j *JSONTree) selectionIncludes(index int) bool {
	if !j.visual {
		return false
	}
	if index < 0 || index >= len(j.lines) {
		return false
	}
	start, end := j.selectionRange()
	return index >= start && index <= end
}

func (j *JSONTree) CurrentScalar() (string, any, bool) {
	if len(j.lines) == 0 || j.cursor < 0 || j.cursor >= len(j.lines) {
		return "", nil, false
	}
	line := j.lines[j.cursor]
	if line.Collapsable {
		return "", nil, false
	}
	return line.Key, line.Value, true
}

func (j *JSONTree) CurrentLine() (jsonLine, bool) {
	if len(j.lines) == 0 || j.cursor < 0 || j.cursor >= len(j.lines) {
		return jsonLine{}, false
	}
	return j.lines[j.cursor], true
}

func (j *JSONTree) SearchNext(matcher *searchMatcher) bool {
	if matcher == nil || len(j.lines) == 0 {
		return false
	}
	start := j.cursor + 1
	for i := 0; i < len(j.lines); i++ {
		idx := (start + i) % len(j.lines)
		line := j.lines[idx]
		fields := map[string]string{
			"path":  line.Path,
			"key":   line.Key,
			"label": line.Label,
			"value": fmt.Sprint(line.Value),
		}
		blob := line.Path + " " + line.Label + " " + fmt.Sprint(line.Value)
		if matcher.MatchFields(fields, blob) {
			j.cursor = idx
			j.ensureCursorVisible(1, j.visibleRows())
			return true
		}
	}
	return false
}

func (j *JSONTree) SearchPrev(matcher *searchMatcher) bool {
	if matcher == nil || len(j.lines) == 0 {
		return false
	}
	start := j.cursor - 1
	for i := 0; i < len(j.lines); i++ {
		idx := (start - i + len(j.lines)*2) % len(j.lines)
		line := j.lines[idx]
		fields := map[string]string{
			"path":  line.Path,
			"key":   line.Key,
			"label": line.Label,
			"value": fmt.Sprint(line.Value),
		}
		blob := line.Path + " " + line.Label + " " + fmt.Sprint(line.Value)
		if matcher.MatchFields(fields, blob) {
			j.cursor = idx
			j.ensureCursorVisible(-1, j.visibleRows())
			return true
		}
	}
	return false
}

func (j *JSONTree) View(height, width int) string {
	if len(j.lines) == 0 {
		return j.title + "\n(empty)"
	}
	if height < 3 {
		height = 3
	}
	visible := max(1, height-1)
	j.viewRows = visible
	j.ensureCursorVisible(0, visible)
	start, end := treeWindowFromTop(len(j.lines), j.scrollTop, visible)

	var b strings.Builder
	b.WriteString(j.title)
	b.WriteString("\n")
	for i := start; i < end; i++ {
		line := j.lines[i]
		cursorPrefix := "  "
		if i == j.cursor {
			cursorPrefix = "> "
		}
		indent := strings.Repeat("  ", line.Depth)
		toggle := "  "
		if line.Collapsable {
			if line.Expanded {
				toggle = "[-]"
			} else {
				toggle = "[+]"
			}
		}
		basePrefix := cursorPrefix + indent + toggle + " "
		available := max(1, width-len([]rune(basePrefix)))
		parts := wrapText(line.Label, available)
		if len(parts) == 0 {
			parts = []string{""}
		}
		continuationPrefix := strings.Repeat(" ", len([]rune(basePrefix)))
		for idx, part := range parts {
			linePrefix := basePrefix
			if idx > 0 {
				linePrefix = continuationPrefix
			}
			rendered := linePrefix + part
			if i == j.cursor && j.selectionIncludes(i) {
				b.WriteString(tableRowCursorVisualStyle.Render(rendered))
			} else if j.selectionIncludes(i) {
				b.WriteString(tableRowVisualStyle.Render(rendered))
			} else {
				b.WriteString(rendered)
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (j *JSONTree) rebuild() {
	lines := make([]jsonLine, 0, 64)
	if ordered, ok := j.root.(OrderedRoot); ok {
		for _, entry := range ordered.Entries {
			buildJSONLines("$."+entry.Key, entry.Key, entry.Value, 0, j.expanded, &lines)
		}
	} else {
		buildJSONLines("$", "root", j.root, 0, j.expanded, &lines)
	}
	j.lines = lines
	if j.cursor >= len(j.lines) {
		j.cursor = max(0, len(j.lines)-1)
	}
	j.ensureCursorVisible(0, j.visibleRows())
}

func (j *JSONTree) visibleRows() int {
	if j.viewRows <= 0 {
		return 1
	}
	return j.viewRows
}

func (j *JSONTree) ensureCursorVisible(moveDir int, rows int) {
	clampCursorAndScroll(len(j.lines), rows, &j.cursor, &j.scrollTop, moveDir)
}

func treeWindowFromTop(total, scrollTop, visible int) (int, int) {
	if visible < 1 {
		visible = 1
	}
	if total <= 0 {
		return 0, 0
	}
	maxTop := max(0, total-visible)
	start := scrollTop
	if start < 0 {
		start = 0
	}
	if start > maxTop {
		start = maxTop
	}
	end := min(total, start+visible)
	return start, end
}

func buildJSONLines(path, key string, value any, depth int, expanded map[string]bool, lines *[]jsonLine) {
	line := jsonLine{Path: path, Depth: depth, Key: key, Label: key + ": " + scalarPreview(value), Value: value}

	switch typed := value.(type) {
	case OrderedRoot:
		line.Collapsable = true
		line.Expanded = expanded[path]
		line.Label = key + fmt.Sprintf(" { %d }", len(typed.Entries))
		*lines = append(*lines, line)
		if !line.Expanded {
			return
		}
		for _, entry := range typed.Entries {
			buildJSONLines(path+"."+entry.Key, entry.Key, entry.Value, depth+1, expanded, lines)
		}
	case map[string]any:
		line.Collapsable = true
		line.Expanded = expanded[path]
		line.Label = key + fmt.Sprintf(" { %d }", len(typed))
		*lines = append(*lines, line)
		if !line.Expanded {
			return
		}
		keys := make([]string, 0, len(typed))
		for k := range typed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			buildJSONLines(path+"."+k, k, typed[k], depth+1, expanded, lines)
		}
	case []any:
		line.Collapsable = true
		line.Expanded = expanded[path]
		line.Label = key + fmt.Sprintf(" [ %d ]", len(typed))
		*lines = append(*lines, line)
		if !line.Expanded {
			return
		}
		for i := range typed {
			idx := fmt.Sprintf("[%d]", i)
			buildJSONLines(path+idx, idx, typed[i], depth+1, expanded, lines)
		}
	default:
		*lines = append(*lines, line)
	}
}

func scalarPreview(v any) string {
	switch t := v.(type) {
	case string:
		return fmt.Sprintf("%q", t)
	case nil:
		return "null"
	default:
		return fmt.Sprint(t)
	}
}

func parentPath(path string) string {
	if path == "$" {
		return "$"
	}
	i := strings.LastIndex(path, ".")
	if i > 0 {
		return path[:i]
	}
	i = strings.LastIndex(path, "[")
	if i > 0 {
		return path[:i]
	}
	return "$"
}

func collectExpandablePaths(path string, value any, expanded map[string]bool) {
	switch t := value.(type) {
	case OrderedRoot:
		expanded[path] = true
		for _, entry := range t.Entries {
			collectExpandablePaths(path+"."+entry.Key, entry.Value, expanded)
		}
	case map[string]any:
		expanded[path] = true
		for k, v := range t {
			collectExpandablePaths(path+"."+k, v, expanded)
		}
	case []any:
		expanded[path] = true
		for i, v := range t {
			collectExpandablePaths(fmt.Sprintf("%s[%d]", path, i), v, expanded)
		}
	}
}
