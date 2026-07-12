package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateReviewRefsRejectsOptionLikeCommit(t *testing.T) {
	err := validateReviewRefs(t.TempDir(), reviewOptions{commit: "-O./pwn.sh"})
	if err == nil {
		t.Fatal("expected option-like --commit ref to be rejected")
	}
	if !strings.Contains(err.Error(), "--commit") || !strings.Contains(err.Error(), "must not start with '-'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateReviewRefsRejectsOptionLikeRangeRef(t *testing.T) {
	err := validateReviewRefs(t.TempDir(), reviewOptions{to: "-O./pwn.sh"})
	if err == nil {
		t.Fatal("expected option-like --to ref to be rejected")
	}
	if !strings.Contains(err.Error(), "--to") || !strings.Contains(err.Error(), "must not start with '-'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseReviewFlagsRejectsToWithoutFrom(t *testing.T) {
	_, err := parseReviewFlags([]string{"--to", "HEAD"})
	if err == nil {
		t.Fatal("expected --to without --from to fail")
	}
	if !strings.Contains(err.Error(), "--from is required when --to is specified") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseReviewFlagsRejectsFromWithoutTo(t *testing.T) {
	_, err := parseReviewFlags([]string{"--from", "main"})
	if err == nil {
		t.Fatal("expected --from without --to to fail")
	}
	if !strings.Contains(err.Error(), "--to is required when --from is specified") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseReviewFlagsAllowsFromAndTo(t *testing.T) {
	opts, err := parseReviewFlags([]string{"--from", "main", "--to", "HEAD"})
	if err != nil {
		t.Fatalf("expected --from/--to to pass, got: %v", err)
	}
	if opts.from != "main" || opts.to != "HEAD" {
		t.Fatalf("unexpected opts: from=%q to=%q", opts.from, opts.to)
	}
}

func TestParseReviewFlagsAllowsReviewContextPath(t *testing.T) {
	opts, err := parseReviewFlags([]string{"--review-context", "/tmp/context.json"})
	if err != nil {
		t.Fatalf("parseReviewFlags: %v", err)
	}
	if opts.reviewContextPath != "/tmp/context.json" {
		t.Fatalf("reviewContextPath = %q", opts.reviewContextPath)
	}
}

func TestRunReviewPreviewRejectsMalformedReviewContext(t *testing.T) {
	repoDir := initReviewTestRepo(t)
	contextPath := writeReviewContextFile(t, `{`)

	err := runReview([]string{"--repo", repoDir, "--preview", "--review-context", contextPath})
	if err == nil {
		t.Fatal("expected malformed review context to be rejected before preview")
	}
	if !strings.Contains(err.Error(), "parse review context") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReviewPreviewAllowsAbsentAndValidReviewContext(t *testing.T) {
	repoDir := initReviewTestRepo(t)
	validContextPath := writeReviewContextFile(t, `{"version":1,"paths":{"README.md":{"rendered":"context"}}}`)

	for _, args := range [][]string{
		{"--repo", repoDir, "--preview"},
		{"--repo", repoDir, "--preview", "--review-context", validContextPath},
	} {
		if err := runReview(args); err != nil {
			t.Fatalf("runReview(%v): %v", args, err)
		}
	}
}

func TestReviewAllSupportedFilesFlagControlsDefaultPathExclusions(t *testing.T) {
	repoDir := initReviewTestRepo(t)
	files := map[string]string{
		"main.go":                   "package main\n",
		"main_test.go":              "package main\n",
		"oh_modules/dependency.ets": "export const value = 1\n",
		"notes.md":                  "not enabled\n",
	}
	for path, content := range files {
		fullPath := filepath.Join(repoDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	defaultOutput := runReviewCommand(t, repoDir)
	if !strings.Contains(defaultOutput, "Will review (1)") || strings.Count(defaultOutput, "default_path") != 2 {
		t.Fatalf("default preview did not preserve exclusions:\n%s", defaultOutput)
	}

	allSupportedOutput := runReviewCommand(t, repoDir, "--review-all-supported-files")
	if !strings.Contains(allSupportedOutput, "Will review (3)") || strings.Contains(allSupportedOutput, "default_path") {
		t.Fatalf("full supported-file preview did not include default paths:\n%s", allSupportedOutput)
	}
	if !strings.Contains(allSupportedOutput, "notes.md") || !strings.Contains(allSupportedOutput, "unsupported_ext") {
		t.Fatalf("full supported-file preview changed extension filtering:\n%s", allSupportedOutput)
	}
	withMarkdownOutput := runReviewCommand(
		t,
		repoDir,
		"--review-all-supported-files",
		"--include-markdown",
	)
	if !strings.Contains(withMarkdownOutput, "Will review (4)") ||
		strings.Contains(withMarkdownOutput, "unsupported_ext") ||
		strings.Contains(withMarkdownOutput, "default_path") {
		t.Fatalf("full supported-file preview did not preserve Markdown opt-in:\n%s", withMarkdownOutput)
	}

	rulePath := filepath.Join(t.TempDir(), "rule.json")
	if err := os.WriteFile(rulePath, []byte(`{"rules":[],"exclude":["main_test.go"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	userExcludedOutput := runReviewCommand(t, repoDir, "--review-all-supported-files", "--rule", rulePath)
	if !strings.Contains(userExcludedOutput, "Will review (2)") ||
		!strings.Contains(userExcludedOutput, "main_test.go") ||
		!strings.Contains(userExcludedOutput, "user_exclude") {
		t.Fatalf("full supported-file preview did not preserve user exclusions:\n%s", userExcludedOutput)
	}
}

func TestLoadReviewContextRejectsMalformedContext(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "invalid JSON",
			body: `{`,
		},
		{
			name: "unsupported version",
			body: `{"version":2,"paths":{}}`,
		},
		{
			name: "missing rendered text",
			body: `{"version":1,"paths":{"src/app.py":{}}}`,
		},
		{
			name: "empty path",
			body: `{"version":1,"paths":{"":{"rendered":"context"}}}`,
		},
		{
			name: "whitespace padded path",
			body: `{"version":1,"paths":{" src/app.py ":{"rendered":"context"}}}`,
		},
		{
			name: "nested revalidation finding",
			body: `{"version":1,"paths":{},"revalidate":[{"instance_id":"fi1:auth","family":{},"evidence":{}}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadReviewContextBody(t, tt.body)
			if err == nil {
				t.Fatal("expected malformed review context to be rejected")
			}
			if !strings.Contains(err.Error(), "review context") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadReviewContextRejectsNonRegularFile(t *testing.T) {
	_, err := loadReviewContext(t.TempDir())
	if err == nil {
		t.Fatal("expected directory review context to be rejected")
	}
	if !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadReviewContextRejectsOversizedContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxReviewContextBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadReviewContext(path)
	if err == nil {
		t.Fatal("expected oversized review context to be rejected")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprint(maxReviewContextBytes)) {
		t.Fatalf("error does not include size limit: %v", err)
	}
}

func TestLoadReviewContextAcceptsFleetContextGraphShape(t *testing.T) {
	body := `{"version":1,"paths":{"src/app.py":{"rendered":"context for app.py","thread_count":1,"omitted_thread_count":0},"src/z.py":{"rendered":"context for z.py","thread_count":2,"omitted_thread_count":1}},"revalidate":[{"instance_id":"fi1:auth","path":"src/app.py","content":"authorization bypass","severity":"high","failure_mode":"unauthorized access","violated_contract":"requests require authorization","evidence":"guard is missing","existing_code":"allow = True","start_line":7,"end_line":7}],"summary":{"included_threads":3,"omitted_threads":1,"included_comments":4,"omitted_comments":2,"omitted_marker":"[Review context omitted: 1 thread(s) overall.]"}}`
	context, err := loadReviewContextBody(t, body)
	if err != nil {
		t.Fatalf("loadReviewContext: %v", err)
	}
	if len(context.Paths) != 2 {
		t.Fatalf("context path count = %d", len(context.Paths))
	}
	if context.Paths["src/app.py"] != "context for app.py" {
		t.Fatalf("src/app.py context = %q", context.Paths["src/app.py"])
	}
	if context.Paths["src/z.py"] != "context for z.py" {
		t.Fatalf("src/z.py context = %q", context.Paths["src/z.py"])
	}
	if len(context.Revalidate) != 1 || context.Revalidate[0].Path != "src/app.py" {
		t.Fatalf("revalidate = %#v", context.Revalidate)
	}
}

func TestLoadReviewContextTreatsAbsentAndEmptyAsNoContext(t *testing.T) {
	if context, err := loadReviewContext(""); err != nil || len(context.Paths) != 0 || len(context.Revalidate) != 0 {
		t.Fatalf("absent context = %#v, %v", context, err)
	}

	context, err := loadReviewContextBody(t, `{"version":1,"paths":{},"summary":{"included_threads":0}}`)
	if err != nil {
		t.Fatalf("loadReviewContext: %v", err)
	}
	if len(context.Paths) != 0 || len(context.Revalidate) != 0 {
		t.Fatalf("empty context = %#v", context)
	}

	context, err = loadReviewContextBody(t, `{"version":1,"paths":{},"revalidate":[{"instance_id":"fi1:auth","path":"src/app.py","content":"authorization bypass","severity":"high","failure_mode":"unauthorized access","violated_contract":"requests require authorization","evidence":"guard is missing","existing_code":"allow = True","start_line":7,"end_line":7}]}`)
	if err != nil || len(context.Revalidate) != 1 {
		t.Fatalf("revalidation-only context = %#v, %v", context, err)
	}
}

func loadReviewContextBody(t *testing.T, body string) (reviewContextInput, error) {
	t.Helper()
	return loadReviewContext(writeReviewContextFile(t, body))
}

func writeReviewContextFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func initReviewTestRepo(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "review@example.com"},
		{"config", "user.name", "Review Test"},
	} {
		runGitForTest(t, repoDir, args...)
	}
	readmePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("review me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "README.md"},
		{"commit", "-m", "initial"},
	} {
		runGitForTest(t, repoDir, args...)
	}
	return repoDir
}

func runReviewCommand(t *testing.T, repoDir string, extraArgs ...string) string {
	t.Helper()
	moduleDir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"run", "./cmd/opencodereview", "review", "--repo", repoDir, "--preview"}
	args = append(args, extraArgs...)
	command := exec.Command("go", args...)
	command.Dir = moduleDir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("ocr review %v: %v\n%s", extraArgs, err, output)
	}
	return string(output)
}

func runGitForTest(t *testing.T, repoDir string, args ...string) {
	t.Helper()
	if out, err := runGitCmd(repoDir, args...); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
