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
	if got := commentSeverity(nil); got != "unclassified" {
		t.Fatalf("commentSeverity(nil) = %q, want unclassified", got)
	}
	if got := commentSeverity("urgent"); got != "unclassified" {
		t.Fatalf("commentSeverity(urgent) = %q, want unclassified", got)
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
