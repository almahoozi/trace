package config

import "testing"

func TestThemeHelpItems_LoadsEmbeddedCatalog(t *testing.T) {
	t.Parallel()

	items, err := ThemeHelpItems()
	if err != nil {
		t.Fatalf("ThemeHelpItems() error = %v", err)
	}
	if len(items) == 0 {
		t.Fatal("ThemeHelpItems() returned no entries")
	}
}

func TestThemeHelpForKey_FindsKnownKeys(t *testing.T) {
	t.Parallel()

	item, ok, err := ThemeHelpForKey("ui.title")
	if err != nil {
		t.Fatalf("ThemeHelpForKey() error = %v", err)
	}
	if !ok {
		t.Fatal("expected ui.title help entry")
	}
	if item.Kind != "color" {
		t.Fatalf("item.Kind = %q, want %q", item.Kind, "color")
	}

	paletteItem, ok, err := ThemeHelpForKey("pallete.text_unfocussed")
	if err != nil {
		t.Fatalf("ThemeHelpForKey() error = %v", err)
	}
	if !ok {
		t.Fatal("expected pallete.text_unfocussed help entry")
	}
	if paletteItem.Kind != "palette" {
		t.Fatalf("palette item.Kind = %q, want %q", paletteItem.Kind, "palette")
	}

	servicePaletteItem, ok, err := ThemeHelpForKey("service_palette")
	if err != nil {
		t.Fatalf("ThemeHelpForKey() error = %v", err)
	}
	if !ok {
		t.Fatal("expected service_palette help entry")
	}
	if servicePaletteItem.Kind != "palette" {
		t.Fatalf("service palette item.Kind = %q, want %q", servicePaletteItem.Kind, "palette")
	}
}
