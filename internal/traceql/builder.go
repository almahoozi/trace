package traceql

import (
	"strconv"
	"strings"
)

var supportedOps = []string{"=~", "!~", ">=", "<=", "!=", "=", ">", "<", ":"}

type ValueType string

const (
	ValueTypeAuto     ValueType = "auto"
	ValueTypeString   ValueType = "string"
	ValueTypeInt      ValueType = "int"
	ValueTypeFloat    ValueType = "float"
	ValueTypeBool     ValueType = "bool"
	ValueTypeDuration ValueType = "duration"
	ValueTypeEnum     ValueType = "enum"
)

type TypedClause struct {
	Field string
	Op    string
	Value string
	Type  ValueType
}

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

func CompileTypedClauses(clauses []TypedClause) string {
	parts := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		normalized := normalizeTypedClause(clause)
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

func normalizeTypedClause(clause TypedClause) string {
	field := normalizeField(clause.Field)
	if field == "" {
		return ""
	}
	op := strings.TrimSpace(clause.Op)
	if op == "" {
		return ""
	}
	if op == ":" {
		op = "="
	}
	value := normalizeValueWithType(op, clause.Value, clause.Type)
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return field + op + value
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
	return normalizeValueWithType(op, raw, ValueTypeAuto)
}

func normalizeValueWithType(op, raw string, typ ValueType) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return v
	}

	switch normalizeValueType(typ) {
	case ValueTypeString:
		if isQuoted(v) {
			return strconv.Quote(unquoteLiteral(v))
		}
		return strconv.Quote(v)
	case ValueTypeEnum:
		dequoted := strings.TrimSpace(unquoteLiteral(v))
		if dequoted == "" {
			return `""`
		}
		return dequoted
	case ValueTypeInt:
		dequoted := strings.TrimSpace(unquoteLiteral(v))
		if _, err := strconv.ParseInt(dequoted, 10, 64); err == nil {
			return dequoted
		}
		return dequoted
	case ValueTypeFloat:
		dequoted := strings.TrimSpace(unquoteLiteral(v))
		if _, err := strconv.ParseFloat(dequoted, 64); err == nil {
			return dequoted
		}
		return dequoted
	case ValueTypeBool:
		dequoted := strings.ToLower(strings.TrimSpace(unquoteLiteral(v)))
		if dequoted == "true" || dequoted == "false" {
			return dequoted
		}
		return dequoted
	case ValueTypeDuration:
		dequoted := strings.TrimSpace(unquoteLiteral(v))
		if isDuration(dequoted) {
			return dequoted
		}
		return dequoted
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

func normalizeValueType(typ ValueType) ValueType {
	switch ValueType(strings.ToLower(strings.TrimSpace(string(typ)))) {
	case ValueTypeString:
		return ValueTypeString
	case ValueTypeInt:
		return ValueTypeInt
	case ValueTypeFloat:
		return ValueTypeFloat
	case ValueTypeBool:
		return ValueTypeBool
	case ValueTypeDuration:
		return ValueTypeDuration
	case ValueTypeEnum:
		return ValueTypeEnum
	default:
		return ValueTypeAuto
	}
}

func unquoteLiteral(v string) string {
	if len(v) < 2 {
		return v
	}
	if v[0] == '`' && v[len(v)-1] == '`' {
		return v[1 : len(v)-1]
	}
	unquoted, err := strconv.Unquote(v)
	if err != nil {
		return v
	}
	return unquoted
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
