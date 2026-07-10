package tool

import (
	"encoding/json"
	"testing"

	"github.com/open-code-review/open-code-review/internal/model"
)

func TestCommentSeverity(t *testing.T) {
	if got := commentSeverity("critical"); got != "critical" {
		t.Fatalf("commentSeverity(critical) = %q, want critical", got)
	}
	if got := commentSeverity(" High "); got != "high" {
		t.Fatalf("commentSeverity(High) = %q, want high", got)
	}
	if got := commentSeverity(nil); got != "" {
		t.Fatalf("commentSeverity(nil) = %q, want empty", got)
	}
	if got := commentSeverity("urgent"); got != "" {
		t.Fatalf("commentSeverity(urgent) = %q, want empty", got)
	}
}

func TestLlmCommentJSONIncludesSeverity(t *testing.T) {
	payload, err := json.Marshal(model.LlmComment{
		Path:         "app.go",
		Content:      "Check authorization before returning data.",
		ExistingCode: "return data, nil",
		StartLine:    12,
		EndLine:      12,
		Severity:     "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["severity"] != "high" {
		t.Fatalf("severity = %v, want high in %s", decoded["severity"], payload)
	}
}

func TestParseCommentsRejectsFindingWithoutVerificationContract(t *testing.T) {
	tests := []struct {
		name, path  string
		rawComments []any
	}{
		{name: "missing contract", path: "app.go", rawComments: []any{map[string]any{
			"content": "Check authorization before returning data.", "severity": "high", "existing_code": "return data, nil",
		}}},
		{name: "non-object entry", path: "app.go", rawComments: []any{42}},
		{name: "invalid severity", path: "app.go", rawComments: []any{map[string]any{
			"content": "Finding", "severity": "urgent", "failure_mode": "failure", "violated_contract": "contract", "evidence": "evidence", "existing_code": "return data, nil",
		}}},
		{name: "missing path", rawComments: []any{map[string]any{
			"content": "Finding", "severity": "high", "failure_mode": "failure", "violated_contract": "contract", "evidence": "evidence", "existing_code": "return data, nil",
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			arguments := map[string]any{"path": test.path, "comments": test.rawComments}
			comments, errMessage := ParseComments(arguments)
			if len(comments) != 0 || errMessage == "" || test.path == "" && errMessage != "Error: missing top-level 'path' argument or invalid comment: every comment requires a valid severity, content, failure_mode, violated_contract, evidence, and existing_code" {
				t.Fatalf("comments = %#v; error = %q, want fail-closed rejection", comments, errMessage)
			}
		})
	}
}
