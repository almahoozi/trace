package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type searchMatcher struct {
	raw      string
	mode     string
	exact    string
	contains string
	re       *regexp.Regexp
	clauses  []searchClause
}

type searchClause struct {
	path  string
	op    string
	value string
	re    *regexp.Regexp
}

func compileSearchMatcher(raw string) (*searchMatcher, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	if re, ok, err := parseRegexLiteral(trimmed); ok {
		if err != nil {
			return nil, err
		}
		return &searchMatcher{raw: trimmed, mode: "regex", re: re}, nil
	}

	if looksLikeFieldQuery(trimmed) {
		clauses, err := parseFieldClauses(trimmed)
		if err != nil {
			return nil, err
		}
		return &searchMatcher{raw: trimmed, mode: "field", clauses: clauses}, nil
	}

	if strings.HasPrefix(trimmed, "=") {
		return &searchMatcher{raw: trimmed, mode: "exact", exact: strings.TrimSpace(trimmed[1:])}, nil
	}
	if quoted, ok := unquotePair(trimmed); ok {
		return &searchMatcher{raw: trimmed, mode: "exact", exact: quoted}, nil
	}

	return &searchMatcher{raw: trimmed, mode: "contains", contains: strings.ToLower(trimmed)}, nil
}

func (m *searchMatcher) MatchText(text string) bool {
	if m == nil {
		return true
	}
	switch m.mode {
	case "regex":
		return m.re != nil && m.re.MatchString(text)
	case "exact":
		return strings.EqualFold(strings.TrimSpace(text), strings.TrimSpace(m.exact))
	case "contains":
		return strings.Contains(strings.ToLower(text), m.contains)
	default:
		return false
	}
}

func (m *searchMatcher) MatchFields(fields map[string]string, blob string) bool {
	if m == nil {
		return true
	}
	if m.mode != "field" {
		return m.MatchText(blob)
	}
	for _, clause := range m.clauses {
		value, ok := lookupField(fields, clause.path)
		if !ok {
			return false
		}
		switch clause.op {
		case "=", "==":
			if !strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(clause.value)) {
				return false
			}
		case "!=":
			if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(clause.value)) {
				return false
			}
		case "~=":
			if clause.re == nil || !clause.re.MatchString(value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func flattenAny(prefix string, value any, out map[string]string) {
	key := strings.TrimSpace(prefix)
	if key != "" {
		out[key] = fmt.Sprint(value)
	}
	switch t := value.(type) {
	case map[string]any:
		for k, v := range t {
			next := k
			if key != "" {
				next = key + "." + k
			}
			flattenAny(next, v, out)
		}
	case []any:
		for i, v := range t {
			next := fmt.Sprintf("%s[%d]", key, i)
			if key == "" {
				next = fmt.Sprintf("[%d]", i)
			}
			flattenAny(next, v, out)
		}
	}
}

func lookupField(fields map[string]string, path string) (string, bool) {
	if fields == nil {
		return "", false
	}
	normalized := normalizeFieldPath(path)
	if v, ok := fields[normalized]; ok {
		return v, true
	}
	for k, v := range fields {
		if normalizeFieldPath(k) == normalized {
			return v, true
		}
	}
	return "", false
}

func normalizeFieldPath(path string) string {
	out := strings.TrimSpace(path)
	out = strings.TrimPrefix(out, "$")
	out = strings.TrimPrefix(out, ".")
	return strings.ToLower(out)
}

func looksLikeFieldQuery(raw string) bool {
	if strings.Contains(raw, "&&") {
		return true
	}
	if strings.Contains(raw, "~=") || strings.Contains(raw, "!=") || strings.Contains(raw, "==") {
		return true
	}
	return strings.Contains(raw, "$.") && strings.Contains(raw, "=")
}

func parseFieldClauses(raw string) ([]searchClause, error) {
	parts := strings.Split(raw, "&&")
	clauses := make([]searchClause, 0, len(parts))
	for _, part := range parts {
		clauseRaw := strings.TrimSpace(part)
		if clauseRaw == "" {
			continue
		}
		path, op, value, err := splitClause(clauseRaw)
		if err != nil {
			return nil, err
		}
		clause := searchClause{path: path, op: op, value: value}
		if op == "~=" {
			re, ok, err := parseRegexLiteral(value)
			if err != nil {
				return nil, err
			}
			if ok {
				clause.re = re
			} else {
				compiled, err := regexp.Compile(value)
				if err != nil {
					return nil, fmt.Errorf("invalid regex in %q: %w", clauseRaw, err)
				}
				clause.re = compiled
			}
		}
		clauses = append(clauses, clause)
	}
	if len(clauses) == 0 {
		return nil, fmt.Errorf("invalid field query")
	}
	return clauses, nil
}

func splitClause(raw string) (string, string, string, error) {
	ops := []string{"~=", "!=", "==", "="}
	for _, op := range ops {
		if idx := strings.Index(raw, op); idx > 0 {
			left := strings.TrimSpace(raw[:idx])
			right := strings.TrimSpace(raw[idx+len(op):])
			if left == "" || right == "" {
				return "", "", "", fmt.Errorf("invalid clause %q", raw)
			}
			if unquoted, ok := unquotePair(right); ok {
				right = unquoted
			}
			return normalizeFieldPath(left), op, right, nil
		}
	}
	return "", "", "", fmt.Errorf("invalid clause %q", raw)
}

func parseRegexLiteral(raw string) (*regexp.Regexp, bool, error) {
	if !strings.HasPrefix(raw, "/") || len(raw) < 2 {
		return nil, false, nil
	}
	last := lastUnescapedSlash(raw)
	if last <= 0 {
		return nil, false, nil
	}
	body := raw[1:last]
	flags := raw[last+1:]
	prefix := ""
	for _, flag := range flags {
		switch flag {
		case 'i':
			prefix += "(?i)"
		case 'm':
			prefix += "(?m)"
		case 's':
			prefix += "(?s)"
		case ' ', '\t':
			continue
		default:
			return nil, true, fmt.Errorf("unsupported regex flag %q", string(flag))
		}
	}
	re, err := regexp.Compile(prefix + body)
	if err != nil {
		return nil, true, fmt.Errorf("invalid regex %q: %w", raw, err)
	}
	return re, true, nil
}

func lastUnescapedSlash(raw string) int {
	if len(raw) < 2 {
		return -1
	}
	escaped := false
	last := -1
	for i := 1; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '/' {
			last = i
		}
	}
	return last
}

func unquotePair(raw string) (string, bool) {
	if len(raw) < 2 {
		return "", false
	}
	if (raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'') {
		unquoted, err := strconv.Unquote(raw)
		if err != nil {
			return raw[1 : len(raw)-1], true
		}
		return unquoted, true
	}
	return "", false
}
