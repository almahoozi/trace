package main

import (
	"testing"

	"github.com/almahoozi/trace/internal/config"
)

func TestExtractInlineFlags_ParsesThemeFlags(t *testing.T) {
	t.Parallel()

	flags, args, err := extractInlineFlags([]string{"theme", "import", "./x.json", "-f", "-y", "--full", "-n", "solarized"})
	if err != nil {
		t.Fatalf("extractInlineFlags() error = %v", err)
	}
	if len(args) != 3 {
		t.Fatalf("cleaned args length = %d, want 3", len(args))
	}
	if !flags.force {
		t.Fatal("expected force flag to be true")
	}
	if !flags.assumeYes {
		t.Fatal("expected assumeYes flag to be true")
	}
	if flags.themeName != "solarized" {
		t.Fatalf("themeName = %q, want %q", flags.themeName, "solarized")
	}
	if !flags.themeFullMode {
		t.Fatal("expected themeFullMode to be true")
	}
}

func TestInferThemeNameFromPath(t *testing.T) {
	t.Parallel()

	if got := inferThemeNameFromPath("./themes/retro-light.json"); got != "retro-light" {
		t.Fatalf("inferThemeNameFromPath() = %q, want %q", got, "retro-light")
	}
}

func TestFilterThemeHelpItemsByActiveTheme(t *testing.T) {
	t.Parallel()

	items := []config.ThemeHelpItem{
		{Key: "ui.title", Kind: "color", Description: "title"},
		{Key: "pallete.text_unfocussed", Kind: "palette", Description: "muted"},
		{Key: "service_palette", Kind: "palette", Description: "services"},
		{Key: "ui.summary.error", Kind: "color", Description: "error"},
	}
	resolved := config.ResolvedTheme{
		Selected: config.ThemePalette{
			Colors:         map[string]string{"ui.title": "12"},
			Pallete:        map[string]string{"text_unfocussed": "245"},
			ServicePalette: []string{"68", "173"},
		},
	}

	filtered := filterThemeHelpItemsByActiveTheme(items, resolved)
	if len(filtered) != 3 {
		t.Fatalf("filtered length = %d, want 3", len(filtered))
	}
}
