package traceql

import "testing"

func TestCompileClauses_AutoModeKeepsCurrentBehavior(t *testing.T) {
	got := CompileClauses([]string{"status=error", "span.http.status_code=500"})
	want := `{status="error" && span.http.status_code=500}`
	if got != want {
		t.Fatalf("CompileClauses() = %q, want %q", got, want)
	}
}

func TestCompileTypedClauses_EnumIsUnquoted(t *testing.T) {
	got := CompileTypedClauses([]TypedClause{{Field: "status", Op: "=", Value: `"error"`, Type: ValueTypeEnum}})
	want := `{status=error}`
	if got != want {
		t.Fatalf("CompileTypedClauses() = %q, want %q", got, want)
	}
}

func TestCompileTypedClauses_ExplicitTypes(t *testing.T) {
	got := CompileTypedClauses([]TypedClause{
		{Field: "service.name", Op: "=", Value: "api", Type: ValueTypeString},
		{Field: "span.http.status_code", Op: "=", Value: `"500"`, Type: ValueTypeInt},
		{Field: "span.error", Op: "=", Value: "TRUE", Type: ValueTypeBool},
	})
	want := `{service.name="api" && span.http.status_code=500 && span.error=true}`
	if got != want {
		t.Fatalf("CompileTypedClauses() = %q, want %q", got, want)
	}
}
