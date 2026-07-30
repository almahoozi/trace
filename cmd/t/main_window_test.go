package main

import (
	"testing"
	"time"
)

func TestParseBrowseWindowOverride_DurationOnlyUsesSince(t *testing.T) {
	t.Parallel()

	window, hasOverride, err := parseBrowseWindowOverride("", "15m")
	if err != nil {
		t.Fatalf("parse duration-only override: %v", err)
	}
	if !hasOverride {
		t.Fatal("expected duration-only override to be enabled")
	}
	if window.Since != 15*time.Minute {
		t.Fatalf("expected since=15m, got %s", window.Since)
	}
	if window.HasStartAt || window.HasEndAt {
		t.Fatal("expected explicit start/end to be unset for duration-only override")
	}
}

func TestParseBrowseWindowOverride_DurationOnlyRejectsInvalid(t *testing.T) {
	t.Parallel()

	_, _, err := parseBrowseWindowOverride("", "15")
	if err == nil {
		t.Fatal("expected invalid duration without unit to fail")
	}
}
