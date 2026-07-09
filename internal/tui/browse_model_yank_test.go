package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/almahoozi/trace/internal/config"
	"github.com/almahoozi/trace/internal/domain"
)

func TestBrowseYank_CopiesCurrentTraceID(t *testing.T) {
	items := []domain.TraceListItem{
		{TraceID: "trace-1"},
		{TraceID: "trace-2"},
	}
	m := BrowseModel{
		cfg:      config.DefaultConfig(),
		items:    items,
		filtered: items,
		cursor:   1,
	}

	var copied string
	original := clipboardWriteAll
	clipboardWriteAll = func(v string) error {
		copied = v
		return nil
	}
	t.Cleanup(func() {
		clipboardWriteAll = original
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	next, ok := updated.(BrowseModel)
	if !ok {
		t.Fatalf("expected BrowseModel after update")
	}

	if copied != "trace-2" {
		t.Fatalf("expected copied trace id, got %q", copied)
	}
	if next.status != "copied trace id" {
		t.Fatalf("unexpected status: %q", next.status)
	}
}

func TestBrowseYank_MultiSelectCopiesRows(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 30, 0, 0, time.UTC)
	items := []domain.TraceListItem{
		{TraceID: "trace-1", OperationName: "GET /one", Service: "svc-a", ErrorSpanCount: 1, SpanCount: 3, Duration: 123 * time.Millisecond, StartTime: now},
		{TraceID: "trace-2", OperationName: "POST /two", Service: "svc-b", ErrorSpanCount: 0, SpanCount: 5, Duration: 456 * time.Millisecond, StartTime: now.Add(time.Second)},
	}
	m := BrowseModel{
		cfg:      config.DefaultConfig(),
		loc:      time.UTC,
		items:    items,
		filtered: items,
		cursor:   0,
	}

	var copied string
	original := clipboardWriteAll
	clipboardWriteAll = func(v string) error {
		copied = v
		return nil
	}
	t.Cleanup(func() {
		clipboardWriteAll = original
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
	next, ok := updated.(BrowseModel)
	if !ok {
		t.Fatalf("expected BrowseModel after toggle")
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	next, ok = updated.(BrowseModel)
	if !ok {
		t.Fatalf("expected BrowseModel after move")
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	next, ok = updated.(BrowseModel)
	if !ok {
		t.Fatalf("expected BrowseModel after yank")
	}

	lines := strings.Split(copied, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 copied rows, got %d: %q", len(lines), copied)
	}
	if !strings.Contains(lines[0], "trace-1") || !strings.Contains(lines[1], "trace-2") {
		t.Fatalf("expected copied rows to include trace IDs, got %q", copied)
	}
	if next.visualMode {
		t.Fatalf("expected highlight mode to be disabled after yank")
	}
	if next.status != "copied 2 rows" {
		t.Fatalf("unexpected status: %q", next.status)
	}
}
