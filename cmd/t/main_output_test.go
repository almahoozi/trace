package main

import (
	"testing"

	"github.com/almahoozi/trace/internal/app"
)

func TestParseOutputOptions_DefaultViewsByFormat(t *testing.T) {
	t.Parallel()

	jsonOpts, err := parseOutputOptions("-", false, "json", "")
	if err != nil {
		t.Fatalf("parse json output options: %v", err)
	}
	if jsonOpts.view != app.OutputViewAll {
		t.Fatalf("expected json default view %q, got %q", app.OutputViewAll, jsonOpts.view)
	}

	textOpts, err := parseOutputOptions("-", false, "text", "")
	if err != nil {
		t.Fatalf("parse text output options: %v", err)
	}
	if textOpts.view != app.OutputViewTrace {
		t.Fatalf("expected text default view %q, got %q", app.OutputViewTrace, textOpts.view)
	}

	htmlOpts, err := parseOutputOptions("-", false, "html", "")
	if err != nil {
		t.Fatalf("parse html output options: %v", err)
	}
	if htmlOpts.view != app.OutputViewAll {
		t.Fatalf("expected html default view %q, got %q", app.OutputViewAll, htmlOpts.view)
	}
}

func TestParseOutputOptions_RejectsConflictingDestinations(t *testing.T) {
	t.Parallel()

	_, err := parseOutputOptions("out.json", true, "json", "")
	if err == nil {
		t.Fatal("expected parseOutputOptions to reject --output + --stdout")
	}
}

func TestExtractInlineFlags_ParsesOutputFlags(t *testing.T) {
	t.Parallel()

	flags, args, err := extractInlineFlags([]string{"prd", "deadbeef", "--output=result.json", "--format=text", "--view=logs", "--stdout"})
	if err != nil {
		t.Fatalf("extract inline flags: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 cleaned args, got %d", len(args))
	}
	if flags.outputPath != "result.json" {
		t.Fatalf("expected output path result.json, got %q", flags.outputPath)
	}
	if flags.outputFormat != "text" {
		t.Fatalf("expected output format text, got %q", flags.outputFormat)
	}
	if flags.outputView != "logs" {
		t.Fatalf("expected output view logs, got %q", flags.outputView)
	}
	if !flags.outputStdout {
		t.Fatal("expected output stdout flag to be true")
	}
}

func TestParseOutputOptions_FormatAliases(t *testing.T) {
	t.Parallel()

	txtOpts, err := parseOutputOptions("-", false, "txt", "")
	if err != nil {
		t.Fatalf("parse txt alias: %v", err)
	}
	if txtOpts.format != app.OutputFormatText {
		t.Fatalf("expected txt alias to map to text, got %q", txtOpts.format)
	}

	imgOpts, err := parseOutputOptions("-", false, "img", "")
	if err != nil {
		t.Fatalf("parse img alias: %v", err)
	}
	if imgOpts.format != app.OutputFormatImage {
		t.Fatalf("expected img alias to map to image, got %q", imgOpts.format)
	}
}

func TestApplyImplicitOutputMode_DefaultNonTTYFallback(t *testing.T) {
	t.Parallel()

	opts := outputOptions{format: app.OutputFormatJSON, view: app.OutputViewAll}
	updated := applyImplicitOutputMode(opts, cliMode{traceID: "abc123"}, false, false, false)
	if !updated.enabled || !updated.toStdout {
		t.Fatalf("expected non-tty fallback to enable stdout output: %#v", updated)
	}
	if updated.format != app.OutputFormatText {
		t.Fatalf("expected non-tty fallback format text, got %q", updated.format)
	}
	if updated.view != app.OutputViewTrace {
		t.Fatalf("expected non-tty fallback view trace, got %q", updated.view)
	}
}

func TestApplyImplicitOutputMode_FormatWithoutDestinationUsesTraceFile(t *testing.T) {
	t.Parallel()

	opts := outputOptions{format: app.OutputFormatSVG, view: app.OutputViewAll}
	updated := applyImplicitOutputMode(opts, cliMode{traceID: "deadbeef"}, true, false, true)
	if !updated.enabled {
		t.Fatal("expected implicit file output to be enabled")
	}
	if updated.toStdout {
		t.Fatal("expected implicit file output, not stdout")
	}
	if updated.path != "deadbeef.svg" {
		t.Fatalf("expected deadbeef.svg, got %q", updated.path)
	}
}
