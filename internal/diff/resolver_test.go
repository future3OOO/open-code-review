package diff

import (
	"testing"

	"github.com/argus-review/argus/internal/model"
)

const testDiff = `diff --git a/pkg/example/handler.go b/pkg/example/handler.go
--- a/pkg/example/handler.go
+++ b/pkg/example/handler.go
@@ -10,7 +10,7 @@ func HandleRequest(w http.ResponseWriter, r *http.Request) {
     ctx := r.Context()
-    log.Print("handling request")
+    log.Printf("handling request: %s", r.URL.Path)
     err := process(ctx)`

func TestResolveLineNumbers_SingleLineHunkMatch(t *testing.T) {
	diffs := []model.Diff{
		{NewPath: "pkg/example/handler.go", Diff: testDiff},
	}
	comments := []model.LlmComment{
		{Path: "pkg/example/handler.go", ExistingCode: `    log.Print("handling request")`},
	}

	result := ResolveLineNumbers(comments, diffs)
	if len(result) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(result))
	}
	cm := result[0]
	if cm.StartLine == 0 || cm.EndLine == 0 {
		t.Errorf("expected non-zero line numbers, got StartLine=%d EndLine=%d", cm.StartLine, cm.EndLine)
	}
	// The existing code is at old-file line 11.
	// Diff: @@ -10,7 → context "ctx := r.Context()" is old line 10, offset becomes 1.
	// Then deleted line "log.Print..." matches → OldStart(10) + offset-before-match...
	// Actually offset increments AFTER each FROM-side check, so it's still 0 when we hit line 0 (context).
	// After context line, offset=1. Deleted line at index 1 tries match with offset=1 → startLine=11.
	// Wait — need to trace carefully: ctx line is HunkContext, offset++ makes it 1 before next iteration.
	// So deleted line sees offset=1, startLine = 10+1 = 11 ✓
	if cm.StartLine != 11 || cm.EndLine != 11 {
		t.Errorf("expected 11..11, got %d..%d", cm.StartLine, cm.EndLine)
	}
}

func TestResolveLineNumbers_WhitespaceTolerant(t *testing.T) {
	diffs := []model.Diff{
		{NewPath: "pkg/example/handler.go", Diff: testDiff},
	}
	// LLM may return indented or differently formatted code
	comments := []model.LlmComment{
		{Path: "pkg/example/handler.go", ExistingCode: `log.Print("handling request")`},
	}

	result := ResolveLineNumbers(comments, diffs)
	cm := result[0]
	if cm.StartLine != 11 || cm.EndLine != 11 {
		t.Errorf("whitespace-tolerant match: expected 11..11, got %d..%d", cm.StartLine, cm.EndLine)
	}
}

func TestResolveLineNumbers_MultiLineHunkMatch(t *testing.T) {
	rawMulti := `diff --git a/test.go b/test.go
--- a/test.go
+++ b/test.go
@@ -5,4 +5,4 @@ import "fmt"
 func foo() {
-    x := 1
-    y := 2
+    x := 10
+    y := 20
 }`

	diffs := []model.Diff{
		{NewPath: "test.go", Diff: rawMulti},
	}
	comments := []model.LlmComment{
		{Path: "test.go", ExistingCode: `    x := 1
    y := 2`},
	}

	result := ResolveLineNumbers(comments, diffs)
	cm := result[0]
	if cm.StartLine == 0 || cm.EndLine == 0 {
		t.Errorf("multiline hunk match: expected non-zero lines, got StartLine=%d EndLine=%d", cm.StartLine, cm.EndLine)
	}
	if cm.StartLine != 6 || cm.EndLine != 7 {
		t.Errorf("expected 6..7, got %d..%d", cm.StartLine, cm.EndLine)
	}
}

func TestResolveLineNumbers_FallbackToFileContent(t *testing.T) {
	// Code that doesn't appear in diff hunks but exists in file content
	raw := `diff --git a/test.go b/test.go
--- a/test.go
+++ b/test.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
 func foo() {}`

	diffs := []model.Diff{
		{NewPath: "test.go", Diff: raw, NewFileContent: `package main
import "fmt"
func foo() {}`},
	}
	comments := []model.LlmComment{
		{Path: "test.go", ExistingCode: `package main
import "fmt"`},
	}

	result := ResolveLineNumbers(comments, diffs)
	cm := result[0]
	// Fallback should find these consecutive lines starting at line 1
	if cm.StartLine != 1 || cm.EndLine != 2 {
		t.Errorf("fallback: expected 1..2, got %d..%d", cm.StartLine, cm.EndLine)
	}
}

func TestResolveLineNumbers_NoMatchKeepsZero(t *testing.T) {
	diffs := []model.Diff{
		{NewPath: "test.go", Diff: testDiff},
	}
	comments := []model.LlmComment{
		{Path: "test.go", ExistingCode: `totally unrelated code`},
	}

	result := ResolveLineNumbers(comments, diffs)
	cm := result[0]
	if cm.StartLine != 0 || cm.EndLine != 0 {
		t.Errorf("no match: expected 0..0, got %d..%d", cm.StartLine, cm.EndLine)
	}
}

func TestResolveLineNumbers_NoExistingCode(t *testing.T) {
	diffs := []model.Diff{
		{NewPath: "test.go", Diff: testDiff},
	}
	comments := []model.LlmComment{
		{Path: "test.go", Content: "some comment without existing_code"},
	}

	result := ResolveLineNumbers(comments, diffs)
	if result[0].StartLine != 0 {
		t.Errorf("empty ExistingCode: expected 0, got %d", result[0].StartLine)
	}
}

func TestResolveLineNumbers_PathNotFound(t *testing.T) {
	diffs := []model.Diff{
		{NewPath: "other.go", Diff: testDiff},
	}
	comments := []model.LlmComment{
		{Path: "missing.go", ExistingCode: `some code`},
	}

	result := ResolveLineNumbers(comments, diffs)
	if result[0].StartLine != 0 {
		t.Errorf("path not found: expected 0, got %d", result[0].StartLine)
	}
}

func TestResolveLineNumbers_EmptyInputs(t *testing.T) {
	// No comments
	r1 := ResolveLineNumbers([]model.LlmComment{}, []model.Diff{{}})
	if len(r1) != 0 {
		t.Errorf("empty comments: expected 0 results, got %d", len(r1))
	}

	// No diffs — returns comments unchanged (line numbers stay at 0)
	r2 := ResolveLineNumbers([]model.LlmComment{{}}, []model.Diff{})
	if len(r2) != 1 || r2[0].StartLine != 0 {
		t.Errorf("empty diffs: expected 1 result with StartLine=0, got %d", len(r2))
	}
}

func TestNormalizeLine(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  hello  ", "hello"},
		{"+added line", "added line"},
		{"-deleted line", "deleted line"},
		{"\tindented\t", "indented"},
		{"", ""},
	}

	for _, tt := range tests {
		got := normalizeLine(tt.input)
		if got != tt.want {
			t.Errorf("normalizeLine(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSplitAndNormalize_SkipsEmptyLines(t *testing.T) {
	lines := splitAndNormalize(`line1

line2`)

	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "line1" || lines[1] != "line2" {
		t.Errorf("got %v", lines)
	}
}

func TestResolveFromHunk_ContextLinesOnly(t *testing.T) {
	// When existing_code matches context (unchanged) lines rather than deleted ones
	raw := `diff --git a/test.go b/test.go
--- a/test.go
+++ b/test.go
@@ -3,3 +3,4 @@
 func main() {
     fmt.Println("hello")
+    fmt.Println("world")
 }`

	diffs := []model.Diff{{NewPath: "test.go", Diff: raw}}
	comments := []model.LlmComment{
		{Path: "test.go", ExistingCode: `    fmt.Println("hello")`},
	}

	result := ResolveLineNumbers(comments, diffs)
	cm := result[0]
	if cm.StartLine == 0 {
		t.Errorf("context-line match: expected non-zero start, got 0")
	}
	if cm.StartLine != 4 {
		t.Errorf("expected line 4, got %d", cm.StartLine)
	}
}
