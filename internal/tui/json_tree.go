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
	title    string
	root     any
	expanded map[string]bool
	lines    []jsonLine
	cursor   int
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
}

func (j *JSONTree) MoveDown() {
	if j.cursor < len(j.lines)-1 {
		j.cursor++
	}
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
}

func (j *JSONTree) Collapse() {
	if len(j.lines) == 0 {
		return
	}
	line := j.lines[j.cursor]
	if line.Collapsable && line.Expanded {
		j.expanded[line.Path] = false
		j.rebuild()
		return
	}
	if line.Path != "$" {
		parent := parentPath(line.Path)
		for i := range j.lines {
			if j.lines[i].Path == parent {
				j.cursor = i
				return
			}
		}
	}
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

func (j *JSONTree) View(height int) string {
	if len(j.lines) == 0 {
		return j.title + "\n(empty)"
	}
	if height < 3 {
		height = 3
	}
	start := 0
	if j.cursor >= height-2 {
		start = j.cursor - (height - 3)
	}
	end := start + height - 1
	if end > len(j.lines) {
		end = len(j.lines)
	}

	var b strings.Builder
	b.WriteString(j.title)
	b.WriteString("\n")
	for i := start; i < end; i++ {
		line := j.lines[i]
		prefix := "  "
		if i == j.cursor {
			prefix = "> "
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
		b.WriteString(prefix)
		b.WriteString(indent)
		b.WriteString(toggle)
		b.WriteString(" ")
		b.WriteString(line.Label)
		b.WriteString("\n")
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
}

func buildJSONLines(path, key string, value any, depth int, expanded map[string]bool, lines *[]jsonLine) {
	line := jsonLine{Path: path, Depth: depth, Key: key, Label: key + ": " + scalarPreview(value), Value: value}

	switch typed := value.(type) {
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
