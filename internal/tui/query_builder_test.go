package tui

import (
	"testing"

	"github.com/almahoozi/trace/internal/traceql"
)

func TestQueryBuilderToClauses_PreservesExplicitType(t *testing.T) {
	b := &queryBuilder{
		rows: []queryRow{
			{Field: "status", Operator: "=", Value: `"error"`, Type: string(traceql.ValueTypeEnum)},
		},
	}

	got := traceql.CompileTypedClauses(b.toClauses())
	if got != "{status=error}" {
		t.Fatalf("CompileTypedClauses() = %q, want %q", got, "{status=error}")
	}
}

func TestNormalizeQueryValueType_UnknownFallsBackToAuto(t *testing.T) {
	if got := normalizeQueryValueType("unknown"); got != string(traceql.ValueTypeAuto) {
		t.Fatalf("normalizeQueryValueType() = %q, want %q", got, string(traceql.ValueTypeAuto))
	}
}
