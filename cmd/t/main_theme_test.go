package main

import "testing"

func TestExtractInlineFlags_ParsesThemeFlags(t *testing.T) {
	t.Parallel()

	flags, args, err := extractInlineFlags([]string{"theme", "import", "./x.json", "-f", "-y", "-n", "solarized"})
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
}

func TestInferThemeNameFromPath(t *testing.T) {
	t.Parallel()

	if got := inferThemeNameFromPath("./themes/retro-light.json"); got != "retro-light" {
		t.Fatalf("inferThemeNameFromPath() = %q, want %q", got, "retro-light")
	}
}
