package config

import (
	"testing"
)

func TestResolveTheme_DefaultsToDark(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Theme = ""
	resolved := cfg.ResolveTheme()
	if resolved.Name != "dark" {
		t.Fatalf("ResolveTheme().Name = %q, want %q", resolved.Name, "dark")
	}
	if resolved.Palette.Colors[ThemeColorTitle] == "" {
		t.Fatalf("ResolveTheme() missing %q", ThemeColorTitle)
	}
}

func TestResolveTheme_EnvOverride(t *testing.T) {
	t.Setenv(ThemeEnvVar, "light")

	cfg := DefaultConfig()
	cfg.Theme = "dark"
	resolved := cfg.ResolveTheme()
	if resolved.Name != "light" {
		t.Fatalf("ResolveTheme().Name = %q, want %q", resolved.Name, "light")
	}
	if !resolved.FromEnv {
		t.Fatal("expected FromEnv to be true")
	}
}

func TestResolveTheme_FallsBackToDefaultThemeValues(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Themes["default"] = ThemePalette{Colors: map[string]string{ThemeColorMuted: "123"}}
	cfg.Themes["custom"] = ThemePalette{Colors: map[string]string{ThemeColorTitle: "200"}}
	cfg.Theme = "custom"

	resolved := cfg.ResolveTheme()
	if got := resolved.Palette.Colors[ThemeColorTitle]; got != "200" {
		t.Fatalf("title color = %q, want %q", got, "200")
	}
	if got := resolved.Palette.Colors[ThemeColorMuted]; got != "123" {
		t.Fatalf("muted color = %q, want %q", got, "123")
	}
}

func TestANSIColorCode(t *testing.T) {
	t.Parallel()

	if got := ANSIColorCode("196", ""); got != "38;5;196" {
		t.Fatalf("ANSIColorCode numeric = %q, want %q", got, "38;5;196")
	}
	if got := ANSIColorCode("31", ""); got != "38;5;31" {
		t.Fatalf("ANSIColorCode short numeric = %q, want %q", got, "38;5;31")
	}
	if got := ANSIColorCode("", "90"); got != "38;5;90" {
		t.Fatalf("ANSIColorCode fallback = %q, want %q", got, "38;5;90")
	}
	if got := ANSIColorCode("38;2;1;2;3", ""); got != "38;2;1;2;3" {
		t.Fatalf("ANSIColorCode passthrough = %q, want %q", got, "38;2;1;2;3")
	}
}

func TestThemeNames_IncludeBuiltIns(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Themes = nil
	names := cfg.ThemeNames()
	if len(names) < 3 {
		t.Fatalf("ThemeNames() count = %d, want at least 3", len(names))
	}
	assertContains := func(target string) {
		t.Helper()
		for _, name := range names {
			if name == target {
				return
			}
		}
		t.Fatalf("ThemeNames() missing %q", target)
	}
	assertContains("default")
	assertContains("dark")
	assertContains("light")
}

func TestIsBuiltInTheme(t *testing.T) {
	t.Parallel()

	if !IsBuiltInTheme("dark") {
		t.Fatal("expected dark to be built-in")
	}
	if !IsBuiltInTheme("LIGHT") {
		t.Fatal("expected light (case-insensitive) to be built-in")
	}
	if IsBuiltInTheme("my-custom") {
		t.Fatal("expected my-custom to be non built-in")
	}
}
