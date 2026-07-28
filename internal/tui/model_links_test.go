package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/almahoozi/trace/internal/config"
	"github.com/almahoozi/trace/internal/domain"
)

func TestAtKeyFromTraceOpensSingleLinkedTrace(t *testing.T) {
	span := &domain.Span{
		ID:       "span-a",
		Service:  "svc-a",
		Name:     "op-a",
		Kind:     "server",
		Duration: time.Millisecond,
		Links: []domain.SpanLink{
			{TraceID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SpanID: "span-b"},
		},
	}
	session := testSessionWithSpan("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "prod", span)

	var gotEnv string
	var gotTraceID string
	loader := func(ctx context.Context, environment, traceID string) (*domain.Session, error) {
		gotEnv = environment
		gotTraceID = traceID
		linkedSpan := &domain.Span{ID: "linked-root", Service: "svc-b", Name: "op-b", Kind: "server", Duration: time.Millisecond}
		return testSessionWithSpan(traceID, environment, linkedSpan), nil
	}

	m := NewModelWithLinkedTraceOpener(config.DefaultConfig(), session, nil, nil, loader)

	updated, cmd := m.updateTrace("@")
	next := updated.(Model)
	if cmd == nil {
		t.Fatalf("expected linked trace loader command")
	}
	if !next.loadingLinked {
		t.Fatalf("expected loadingLinked to be true")
	}

	msg := cmd()
	loaded, ok := msg.(linkedTraceLoadedMsg)
	if !ok {
		t.Fatalf("expected linkedTraceLoadedMsg, got %T", msg)
	}
	if gotEnv != "prod" {
		t.Fatalf("expected loader environment prod, got %q", gotEnv)
	}
	if gotTraceID != span.Links[0].TraceID {
		t.Fatalf("expected loader trace id %q, got %q", span.Links[0].TraceID, gotTraceID)
	}

	updated, _ = next.Update(loaded)
	opened := updated.(Model)
	if opened.session == nil || opened.session.Trace == nil {
		t.Fatalf("expected opened session to be set")
	}
	if opened.session.Trace.TraceID != span.Links[0].TraceID {
		t.Fatalf("expected opened trace id %q, got %q", span.Links[0].TraceID, opened.session.Trace.TraceID)
	}
}

func TestAtKeyFromTraceWithMultipleLinksOpensDetailsAtLinks(t *testing.T) {
	span := &domain.Span{
		ID:       "span-a",
		Service:  "svc-a",
		Name:     "op-a",
		Kind:     "server",
		Duration: time.Millisecond,
		Links: []domain.SpanLink{
			{TraceID: "11111111111111111111111111111111", SpanID: "span-1"},
			{TraceID: "22222222222222222222222222222222", SpanID: "span-2"},
		},
	}
	m := NewModel(config.DefaultConfig(), testSessionWithSpan("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "prod", span), nil, nil)

	updated, cmd := m.updateTrace("@")
	next := updated.(Model)
	if cmd != nil {
		t.Fatalf("did not expect linked trace command when multiple links exist")
	}
	if next.jsonTree == nil {
		t.Fatalf("expected span details view to open")
	}
	if next.detailSpanID != span.ID {
		t.Fatalf("expected detailSpanID %q, got %q", span.ID, next.detailSpanID)
	}
	line, ok := next.jsonTree.CurrentLine()
	if !ok {
		t.Fatalf("expected cursor line in details tree")
	}
	if line.Path != "$.links" {
		t.Fatalf("expected cursor to move to links section, got %q", line.Path)
	}
}

func TestAtKeyFromSpanDetailsOpensSelectedLink(t *testing.T) {
	span := &domain.Span{
		ID:       "span-a",
		Service:  "svc-a",
		Name:     "op-a",
		Kind:     "server",
		Duration: time.Millisecond,
		Links: []domain.SpanLink{
			{TraceID: "11111111111111111111111111111111", SpanID: "span-1"},
			{TraceID: "22222222222222222222222222222222", SpanID: "span-2"},
		},
	}

	var gotTraceID string
	loader := func(ctx context.Context, environment, traceID string) (*domain.Session, error) {
		gotTraceID = traceID
		linkedSpan := &domain.Span{ID: "linked-root", Service: "svc-b", Name: "op-b", Kind: "server", Duration: time.Millisecond}
		return testSessionWithSpan(traceID, environment, linkedSpan), nil
	}

	m := NewModelWithLinkedTraceOpener(config.DefaultConfig(), testSessionWithSpan("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "prod", span), nil, nil, loader)
	m.openSpanDetails(span, true)

	selected := false
	for i, line := range m.jsonTree.lines {
		if strings.HasPrefix(line.Path, "$.links.02 ") {
			m.jsonTree.cursor = i
			selected = true
			break
		}
	}
	if !selected {
		t.Fatalf("failed to locate second link row in details tree")
	}

	updated, cmd := m.updateJSON("@")
	next := updated.(Model)
	if cmd == nil {
		t.Fatalf("expected linked trace command from details view")
	}
	if !next.loadingLinked {
		t.Fatalf("expected loadingLinked to be true")
	}

	_ = cmd()
	if gotTraceID != span.Links[1].TraceID {
		t.Fatalf("expected selected link trace id %q, got %q", span.Links[1].TraceID, gotTraceID)
	}
}

func testSessionWithSpan(traceID, environment string, span *domain.Span) *domain.Session {
	trace := &domain.Trace{
		TraceID:     traceID,
		RootSpanIDs: []string{span.ID},
		Spans:       []*domain.Span{span},
		SpansByID:   map[string]*domain.Span{span.ID: span},
		SpanCount:   1,
		StartTime:   time.Now().UTC(),
	}
	return &domain.Session{
		Trace:       trace,
		Environment: environment,
	}
}
