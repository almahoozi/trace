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
