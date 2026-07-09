package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/almahoozi/trace/internal/config"
)

func TestYankSelectionToClipboard_TraceRangeIncludesHierarchy(t *testing.T) {
	m := Model{
		cfg: config.DefaultConfig(),
		traceLines: []traceLine{
			{Depth: 0, Kind: "client", Proxy: true, Service: "rain-api-gateway.rain-api-gateway", Label: "user-server-uae.rain-user.svc.cluster.local:8081/*", Duration: 19254 * time.Microsecond, HasKids: true, XCost: 9492 * time.Microsecond},
			{Depth: 1, Kind: "server", Proxy: true, Service: "rain-user", Label: "user-server-uae.rain-user.svc.cluster.local:8081/*", Duration: 9762 * time.Microsecond},
		},
		traceCursor:  1,
		visualMode:   true,
		visualScope:  selectionScopeTrace,
		visualAnchor: 0,
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

	m.yankSelectionToClipboard()

	if !strings.Contains(copied, "- [cli] [P] [rain-api-gateway.rain-api-gateway] user-server-uae.rain-user.svc.cluster.local:8081/* 19.254ms [9.492ms]") {
		t.Fatalf("missing parent line in copied payload: %q", copied)
	}
	if !strings.Contains(copied, "  - [srv] [P] [rain-user] user-server-uae.rain-user.svc.cluster.local:8081/* 9.762ms") {
		t.Fatalf("missing child line with hierarchy indentation: %q", copied)
	}
}

func TestYankSelectionToClipboard_StripsANSIFromTreeLines(t *testing.T) {
	tree := &JSONTree{
		lines: []jsonLine{{Depth: 0, Label: "\x1b[38;5;68mservice-a\x1b[0m"}},
	}
	m := Model{jsonTree: tree}

	var copied string
	original := clipboardWriteAll
	clipboardWriteAll = func(v string) error {
		copied = v
		return nil
	}
	t.Cleanup(func() {
		clipboardWriteAll = original
	})

	m.yankSelectionToClipboard()

	if copied != "service-a" {
		t.Fatalf("unexpected copied payload: %q", copied)
	}
}

func TestYankSelectionToClipboard_SingleTraceLineHasNoLeadingIndent(t *testing.T) {
	m := Model{
		cfg: config.DefaultConfig(),
		traceLines: []traceLine{
			{Depth: 1, Kind: "server", Proxy: true, Service: "rain-user", Label: "user-server-uae.rain-user.svc.cluster.local:8081/*", Duration: 9762 * time.Microsecond, LinkCount: 1},
		},
	}

	lines, err := m.yankSelectionLines()
	if err != nil {
		t.Fatalf("yankSelectionLines returned error: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected one line, got %d", len(lines))
	}
	if strings.HasPrefix(lines[0], " ") || strings.HasPrefix(lines[0], "\t") {
		t.Fatalf("single line should not be indented: %q", lines[0])
	}
	if !strings.Contains(lines[0], "- @ [srv] [P] [rain-user]") {
		t.Fatalf("expected link marker in single line yank: %q", lines[0])
	}
}

func TestYankSelectionToClipboard_MultiTraceLinesUseRelativeIndentWithLowestBase(t *testing.T) {
	m := Model{
		cfg: config.DefaultConfig(),
		traceLines: []traceLine{
			{Depth: 2, Kind: "server", Service: "svc-a", Label: "A", Duration: time.Millisecond},
			{Depth: 3, Kind: "server", Service: "svc-b", Label: "B", Duration: time.Millisecond},
			{Depth: 1, Kind: "server", Service: "svc-root", Label: "ROOT", Duration: time.Millisecond},
		},
		traceCursor:  2,
		visualMode:   true,
		visualScope:  selectionScopeTrace,
		visualAnchor: 0,
	}

	lines, err := m.yankSelectionLines()
	if err != nil {
		t.Fatalf("yankSelectionLines returned error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected three lines, got %d", len(lines))
	}
	if strings.HasPrefix(lines[2], " ") || strings.HasPrefix(lines[2], "\t") {
		t.Fatalf("lowest-depth line should be at zero indentation: %q", lines[2])
	}
	if !strings.HasPrefix(lines[0], "  ") {
		t.Fatalf("first yanked line should be relative to lowest depth: %q", lines[0])
	}
}

func TestDisableLineHighlightModeOnEsc(t *testing.T) {
	m := Model{
		cfg:         config.DefaultConfig(),
		visualMode:  true,
		visualScope: selectionScopeTrace,
		traceLines:  []traceLine{{Depth: 0, Kind: "server", Service: "svc", Label: "op", Duration: time.Millisecond}},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("expected Model after update")
	}
	if next.visualMode {
		t.Fatalf("expected highlight mode to be disabled on esc")
	}
}

func TestYankSelectionToClipboard_AutoExitsHighlightMode(t *testing.T) {
	tree := &JSONTree{
		lines:  []jsonLine{{Depth: 0, Label: "service-a"}},
		visual: true,
	}
	m := Model{jsonTree: tree}

	original := clipboardWriteAll
	clipboardWriteAll = func(v string) error { return nil }
	t.Cleanup(func() {
		clipboardWriteAll = original
	})

	m.yankSelectionToClipboard()

	if m.jsonTree.visual {
		t.Fatalf("expected tree highlight mode to be disabled after yank")
	}
}
