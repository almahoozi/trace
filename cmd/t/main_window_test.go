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

func TestParseBrowseWindowOverride_DurationOnlyNegativeUsesAbsoluteSince(t *testing.T) {
	t.Parallel()

	window, hasOverride, err := parseBrowseWindowOverride("", "-30m")
	if err != nil {
		t.Fatalf("parse duration-only override: %v", err)
	}
	if !hasOverride {
		t.Fatal("expected duration-only override to be enabled")
	}
	if window.Since != 30*time.Minute {
		t.Fatalf("expected since=30m, got %s", window.Since)
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

func TestParseBrowseWindowOverride_AnchoredPositiveDuration(t *testing.T) {
	t.Parallel()

	window, hasOverride, err := parseBrowseWindowOverride("2026-07-16T10:00:00Z", "30m")
	if err != nil {
		t.Fatalf("parse anchored positive duration: %v", err)
	}
	if !hasOverride {
		t.Fatal("expected override to be enabled")
	}
	if !window.HasStartAt || !window.HasEndAt {
		t.Fatal("expected explicit start/end to be set")
	}
	expectedStart := time.Date(2026, time.July, 16, 10, 0, 0, 0, time.UTC)
	expectedEnd := expectedStart.Add(30 * time.Minute)
	if !window.StartAt.Equal(expectedStart) {
		t.Fatalf("expected start %s, got %s", expectedStart, window.StartAt)
	}
	if !window.EndAt.Equal(expectedEnd) {
		t.Fatalf("expected end %s, got %s", expectedEnd, window.EndAt)
	}
}

func TestParseBrowseWindowOverride_AnchoredNegativeDuration(t *testing.T) {
	t.Parallel()

	window, hasOverride, err := parseBrowseWindowOverride("2026-07-16T10:00:00Z", "-30m")
	if err != nil {
		t.Fatalf("parse anchored negative duration: %v", err)
	}
	if !hasOverride {
		t.Fatal("expected override to be enabled")
	}
	if !window.HasStartAt || !window.HasEndAt {
		t.Fatal("expected explicit start/end to be set")
	}
	expectedEnd := time.Date(2026, time.July, 16, 10, 0, 0, 0, time.UTC)
	expectedStart := expectedEnd.Add(-30 * time.Minute)
	if !window.StartAt.Equal(expectedStart) {
		t.Fatalf("expected start %s, got %s", expectedStart, window.StartAt)
	}
	if !window.EndAt.Equal(expectedEnd) {
		t.Fatalf("expected end %s, got %s", expectedEnd, window.EndAt)
	}
}

func TestParseBrowseWindowOverride_TimeRangeDateOnly(t *testing.T) {
	t.Parallel()

	window, hasOverride, err := parseBrowseWindowOverride("2026-07-01/2026-07-02", "")
	if err != nil {
		t.Fatalf("parse date-only range: %v", err)
	}
	if !hasOverride {
		t.Fatal("expected override to be enabled")
	}
	if !window.HasStartAt || !window.HasEndAt {
		t.Fatal("expected explicit start/end to be set")
	}
	expectedStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.Local).UTC()
	expectedEnd := time.Date(2026, time.July, 2, 23, 59, 59, 0, time.Local).UTC()
	if !window.StartAt.Equal(expectedStart) {
		t.Fatalf("expected start %s, got %s", expectedStart, window.StartAt)
	}
	if !window.EndAt.Equal(expectedEnd) {
		t.Fatalf("expected end %s, got %s", expectedEnd, window.EndAt)
	}
}

func TestParseBrowseWindowOverride_AnchoredLocalDateTimeWithoutTZ(t *testing.T) {
	t.Parallel()

	window, hasOverride, err := parseBrowseWindowOverride("2026-07-16T10:15:30", "30m")
	if err != nil {
		t.Fatalf("parse local datetime without timezone: %v", err)
	}
	if !hasOverride {
		t.Fatal("expected override to be enabled")
	}
	if !window.HasStartAt || !window.HasEndAt {
		t.Fatal("expected explicit start/end to be set")
	}
	localStart := time.Date(2026, time.July, 16, 10, 15, 30, 0, time.Local)
	expectedStart := localStart.UTC()
	expectedEnd := localStart.Add(30 * time.Minute).UTC()
	if !window.StartAt.Equal(expectedStart) {
		t.Fatalf("expected start %s, got %s", expectedStart, window.StartAt)
	}
	if !window.EndAt.Equal(expectedEnd) {
		t.Fatalf("expected end %s, got %s", expectedEnd, window.EndAt)
	}
}
