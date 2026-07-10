package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/open-code-review/open-code-review/internal/agent"
)

func TestSanitizeTerminal(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text unchanged", "hello world", "hello world"},
		{"preserves tab", "col1\tcol2", "col1\tcol2"},
		{"preserves newline", "line1\nline2", "line1\nline2"},
		{"strips ESC", "before\x1b[2Jafter", "before[2Jafter"},
		{"strips OSC 52", "\x1b]52;c;dGVzdA==\x07", "]52;c;dGVzdA=="},
		{"strips BEL alone", "beep\x07done", "beepdone"},
		{"strips null byte", "a\x00b", "ab"},
		{"strips DEL", "a\x7fb", "ab"},
		{"strips carriage return", "fake\rreal", "fakereal"},
		{"empty string", "", ""},
		{"only control chars", "\x1b\x07\x00\x7f", ""},
		{"unicode preserved", "代码审查 レビュー 🔍", "代码审查 レビュー 🔍"},
		{"mixed safe and unsafe", "path\x1b[0m/file.go", "path[0m/file.go"},
		{"strips C1 CSI (U+009B)", "beforeafter", "beforeafter"},
		{"strips C1 OSC (U+009D)", "beforeafter", "beforeafter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeTerminal(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeTerminal(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestJSONOutputMarksCoverageWarningIncomplete(t *testing.T) {
	for _, warnings := range [][]agent.AgentWarning{
		nil,
		{{File: "workflow.drawio", Type: "coverage_incomplete", Message: "unsupported_ext"}},
	} {
		payload := captureJSONOutput(t, func() error {
			return outputJSONWithWarnings(nil, warnings, agent.ReviewCoverage{Status: "incomplete", ChangedFiles: 2, EligibleFiles: 1, ReviewedFiles: 1, ExcludedFiles: 1}, 0, 0, 0, 0, 0, time.Second)
		})
		coverage, ok := payload["coverage"].(map[string]any)
		if !ok || payload["status"] != "completed_with_errors" || coverage["status"] != "incomplete" ||
			payload["summary"].(map[string]any)["files_reviewed"] != coverage["reviewed_files"] {
			t.Fatalf("coverage and summary = %#v, want consistent incomplete result", payload)
		}
	}
}

func TestJSONNoFilesPreservesRevalidationWarning(t *testing.T) {
	payload := captureJSONOutput(t, func() error {
		return outputJSONNoFiles(agent.ReviewCoverage{Status: "incomplete"}, []agent.AgentWarning{{File: "src/app.py", Type: "revalidation_incomplete", Message: "not in delta"}})
	})
	warnings, ok := payload["warnings"].([]any)
	if !ok || payload["status"] != "completed_with_errors" || len(warnings) != 1 || warnings[0].(map[string]any)["file"] != "src/app.py" {
		t.Fatalf("output = %#v, want incomplete result with revalidation path", payload)
	}
}

func captureJSONOutput(t *testing.T, output func() error) map[string]any {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = write
	t.Cleanup(func() { os.Stdout = original })

	if err := output(); err != nil {
		t.Fatal(err)
	}
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = original

	var payload map[string]any
	if err := json.NewDecoder(read).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload
}
