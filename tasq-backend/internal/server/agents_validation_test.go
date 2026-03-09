package server

import (
	"encoding/json"
	"testing"
)

func TestParseRequiredResultPayloadV2RequiresEntries(t *testing.T) {
	payload := map[string]any{
		"summary":    "Implemented the task.",
		"changes":    []string{},
		"paths":      []string{"cmd/ytdl/main.go"},
		"commands":   []string{"go test ./..."},
		"tests":      []string{"go test ./..."},
		"artifacts":  []string{"binary:ytdl"},
		"next_risks": []string{"Need broader fixture coverage."},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if _, err := parseRequiredResultPayload(raw, "v2"); err == nil {
		t.Fatalf("expected empty array validation error")
	}
}

func TestValidateResultMarkdownRequiresSections(t *testing.T) {
	valid := `## Summary
Detailed explanation of what changed and why this handoff matters.

## Changes
- Added a new CLI entrypoint.

## Verification
- Ran go test ./...

## Risks
- Integration coverage is still limited.`
	if err := validateResultMarkdown(valid); err != nil {
		t.Fatalf("expected valid markdown, got %v", err)
	}

	invalid := `plain text result`
	if err := validateResultMarkdown(invalid); err == nil {
		t.Fatalf("expected structured markdown validation error")
	}
}
