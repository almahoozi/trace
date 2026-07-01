package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type queryBuilder struct {
	clauses []string
	index   int
	cursor  int
}

func newQueryBuilder(query string) *queryBuilder {
	clauses := splitQueryClauses(query)
	if len(clauses) == 0 {
		clauses = []string{""}
	}
	last := len([]rune(clauses[0]))
	return &queryBuilder{clauses: clauses, index: 0, cursor: last}
}

func (b *queryBuilder) Update(key tea.KeyMsg) (query string, apply bool, cancel bool) {
	if b == nil {
		return "", false, false
	}
	switch key.String() {
	case "esc":
		return "", false, true
	case "enter", "ctrl+r":
		return compileTraceQLFromClauses(b.clauses), true, false
	case "up":
		if b.index > 0 {
			b.index--
			b.cursor = len([]rune(b.clauses[b.index]))
		}
		return "", false, false
	case "down", "tab":
		if b.index < len(b.clauses)-1 {
			b.index++
			b.cursor = len([]rune(b.clauses[b.index]))
		}
		return "", false, false
	case "shift+tab":
		if b.index > 0 {
			b.index--
			b.cursor = len([]rune(b.clauses[b.index]))
		}
		return "", false, false
	case "ctrl+n":
		insertAt := b.index + 1
		b.clauses = append(b.clauses, "")
		copy(b.clauses[insertAt+1:], b.clauses[insertAt:])
		b.clauses[insertAt] = ""
		b.index = insertAt
		b.cursor = 0
		return "", false, false
	case "ctrl+d":
		if len(b.clauses) <= 1 {
			b.clauses[0] = ""
			b.index = 0
			b.cursor = 0
			return "", false, false
		}
		b.clauses = append(b.clauses[:b.index], b.clauses[b.index+1:]...)
		if b.index >= len(b.clauses) {
			b.index = len(b.clauses) - 1
		}
		b.cursor = len([]rune(b.clauses[b.index]))
		return "", false, false
	case "left":
		if b.cursor > 0 {
			b.cursor--
		}
		return "", false, false
	case "right":
		line := []rune(b.clauses[b.index])
		if b.cursor < len(line) {
			b.cursor++
		}
		return "", false, false
	case "home":
		b.cursor = 0
		return "", false, false
	case "end":
		b.cursor = len([]rune(b.clauses[b.index]))
		return "", false, false
	case "backspace":
		line := []rune(b.clauses[b.index])
		if b.cursor > 0 {
			line = append(line[:b.cursor-1], line[b.cursor:]...)
			b.cursor--
			b.clauses[b.index] = string(line)
		}
		return "", false, false
	case "delete":
		line := []rune(b.clauses[b.index])
		if b.cursor < len(line) {
			line = append(line[:b.cursor], line[b.cursor+1:]...)
			b.clauses[b.index] = string(line)
		}
		return "", false, false
	}

	if key.Type == tea.KeyRunes {
		r := key.Runes
		line := []rune(b.clauses[b.index])
		if b.cursor < 0 {
			b.cursor = 0
		}
		if b.cursor > len(line) {
			b.cursor = len(line)
		}
		updated := append([]rune{}, line[:b.cursor]...)
		updated = append(updated, r...)
		updated = append(updated, line[b.cursor:]...)
		b.clauses[b.index] = string(updated)
		b.cursor += len(r)
	}

	return "", false, false
}

func (b *queryBuilder) View(width int) string {
	if b == nil {
		return ""
	}
	var lines []string
	lines = append(lines, "query builder (enter run | esc cancel | ctrl+n add | ctrl+d delete)")
	for i, clause := range b.clauses {
		prefix := "  "
		if i == b.index {
			prefix = "> "
		}
		text := clause
		if i == b.index {
			r := []rune(clause)
			if b.cursor < 0 {
				b.cursor = 0
			}
			if b.cursor > len(r) {
				b.cursor = len(r)
			}
			if b.cursor == len(r) {
				text = string(r) + "|"
			} else {
				text = string(r[:b.cursor]) + "|" + string(r[b.cursor:])
			}
		}
		if strings.TrimSpace(text) == "" {
			text = "|"
		}
		lines = append(lines, fmt.Sprintf("%s%d) %s", prefix, i+1, text))
	}
	preview := compileTraceQLFromClauses(b.clauses)
	if strings.TrimSpace(preview) == "" {
		preview = "(empty query: list recent traces)"
	}
	lines = append(lines, "preview: "+preview)
	if width <= 0 {
		return strings.Join(lines, "\n")
	}
	for i, line := range lines {
		lines[i] = truncate(line, width)
	}
	return strings.Join(lines, "\n")
}

func splitQueryClauses(query string) []string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		trimmed = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "{"), "}"))
	}
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "&&")
	clauses := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p != "" {
			clauses = append(clauses, p)
		}
	}
	if len(clauses) == 0 {
		return []string{trimmed}
	}
	return clauses
}

func compileTraceQLFromClauses(clauses []string) string {
	parts := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		trimmed := strings.TrimSpace(clause)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
			trimmed = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "{"), "}"))
		}
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, " && ") + "}"
}
