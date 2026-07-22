package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExportNonDefaultWithMessage_AddsMessageWhenSet(t *testing.T) {
	cfg := DefaultConfig()
	payload, err := ExportNonDefaultWithMessage(cfg, "Congratulations, request credentials in #infrastructure-requests")
	if err != nil {
		t.Fatalf("ExportNonDefaultWithMessage() error = %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := strings.TrimSpace(asString(out[importMessageKey])); got == "" {
		t.Fatalf("expected %q key in export payload", importMessageKey)
	}
}

func TestExportNonDefaultWithMessage_OmitsMessageWhenEmpty(t *testing.T) {
	cfg := DefaultConfig()
	payload, err := ExportNonDefaultWithMessage(cfg, "  ")
	if err != nil {
		t.Fatalf("ExportNonDefaultWithMessage() error = %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := out[importMessageKey]; ok {
		t.Fatalf("did not expect %q key in export payload", importMessageKey)
	}
}

func TestDiffImport_IgnoresMessageOnlyOverlay(t *testing.T) {
	cfg := DefaultConfig()
	changes, err := DiffImport(cfg, []byte(`{"message":"hello"}`))
	if err != nil {
		t.Fatalf("DiffImport() error = %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("DiffImport() changes = %d, want 0", len(changes))
	}
}

func TestApplyImport_DoesNotPersistMessage(t *testing.T) {
	cfg := DefaultConfig()
	updated, err := ApplyImport(cfg, []byte(`{"message":"hello","auth":{"token_env":"TRACE_TOKEN"}}`))
	if err != nil {
		t.Fatalf("ApplyImport() error = %v", err)
	}
	if got := updated.Auth.TokenEnv; got != "TRACE_TOKEN" {
		t.Fatalf("updated.Auth.TokenEnv = %q, want %q", got, "TRACE_TOKEN")
	}
	m, err := configToJSONMap(updated)
	if err != nil {
		t.Fatalf("configToJSONMap() error = %v", err)
	}
	if _, ok := m[importMessageKey]; ok {
		t.Fatalf("did not expect %q key persisted in config", importMessageKey)
	}
}

func TestImportMessage_ExtractsMessage(t *testing.T) {
	message, err := ImportMessage([]byte(`{"message":"Congrats!"}`))
	if err != nil {
		t.Fatalf("ImportMessage() error = %v", err)
	}
	if message != "Congrats!" {
		t.Fatalf("ImportMessage() = %q, want %q", message, "Congrats!")
	}
}

func TestExportNonDefault_OmitsThemeFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Theme = "light"
	cfg.Themes["custom"] = ThemePalette{Colors: map[string]string{"ui.title": "12"}}

	payload, err := ExportNonDefault(cfg)
	if err != nil {
		t.Fatalf("ExportNonDefault() error = %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := out["theme"]; ok {
		t.Fatal("did not expect theme key in config export")
	}
	if _, ok := out["themes"]; ok {
		t.Fatal("did not expect themes key in config export")
	}
}

func TestApplyImport_IgnoresThemeFields(t *testing.T) {
	cfg := DefaultConfig()
	originalTheme := cfg.Theme

	updated, err := ApplyImport(cfg, []byte(`{"theme":"light","themes":{"custom":{"colors":{"ui.title":"12"}}},"auth":{"token_env":"TRACE_TOKEN"}}`))
	if err != nil {
		t.Fatalf("ApplyImport() error = %v", err)
	}
	if updated.Theme != originalTheme {
		t.Fatalf("updated.Theme = %q, want %q", updated.Theme, originalTheme)
	}
	if got := updated.Auth.TokenEnv; got != "TRACE_TOKEN" {
		t.Fatalf("updated.Auth.TokenEnv = %q, want %q", got, "TRACE_TOKEN")
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
