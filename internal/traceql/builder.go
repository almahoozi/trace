package traceql

import (
	"strconv"
	"strings"
)

var supportedOps = []string{"=~", "!~", ">=", "<=", "!=", "=", ">", "<", ":"}

func CompileClauses(clauses []string) string {
	parts := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		normalized := normalizeClause(clause)
		if normalized != "" {
			parts = append(parts, normalized)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, " && ") + "}"
}

func SplitClauses(query string) []string {
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

func normalizeClause(clause string) string {
	trimmed := strings.TrimSpace(clause)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		trimmed = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "{"), "}"))
	}
	if trimmed == "" {
		return ""
	}

	lhs, op, rhs, ok := splitClause(trimmed)
	if !ok {
		return normalizeField("name") + "=" + normalizeValue("=", trimmed)
	}
	lhs = normalizeField(lhs)

	if op == ":" {
		op = "="
	}
	rhs = normalizeValue(op, rhs)
	return lhs + op + rhs
}

func normalizeField(field string) string {
	trimmed := strings.TrimSpace(field)
	return trimmed
}

func splitClause(clause string) (lhs string, op string, rhs string, ok bool) {
	for _, candidate := range supportedOps {
		idx := strings.Index(clause, candidate)
		if idx <= 0 {
			continue
		}
		lhs = strings.TrimSpace(clause[:idx])
		rhs = strings.TrimSpace(clause[idx+len(candidate):])
		if lhs == "" || rhs == "" {
			return "", "", "", false
		}
		return lhs, candidate, rhs, true
	}
	return "", "", "", false
}

func normalizeValue(op, raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return v
	}
	if isQuoted(v) {
		return v
	}
	if isNumeric(v) || isBool(v) || isDuration(v) || strings.HasPrefix(v, "$") {
		return v
	}
	if op == "=~" || op == "!~" {
		if strings.HasPrefix(v, "`") && strings.HasSuffix(v, "`") {
			return v
		}
		return strconv.Quote(v)
	}
	return strconv.Quote(v)
}

func isQuoted(v string) bool {
	if len(v) < 2 {
		return false
	}
	if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') || (v[0] == '`' && v[len(v)-1] == '`') {
		return true
	}
	return false
}

func isNumeric(v string) bool {
	_, err := strconv.ParseFloat(v, 64)
	return err == nil
}

func isBool(v string) bool {
	low := strings.ToLower(v)
	return low == "true" || low == "false"
}

func isDuration(v string) bool {
	if len(v) < 2 {
		return false
	}
	last := v[len(v)-1]
	if strings.IndexByte("smhdw", last) == -1 {
		return false
	}
	_, err := strconv.ParseFloat(v[:len(v)-1], 64)
	return err == nil
}
