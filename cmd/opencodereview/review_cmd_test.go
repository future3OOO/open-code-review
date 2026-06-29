package main

import (
	"os"
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
}

func TestLoadReviewContextAcceptsFleetContextGraphShape(t *testing.T) {
	body := `{"version":1,"paths":{"src/app.py":{"rendered":"context for app.py","thread_count":1,"omitted_thread_count":0},"src/z.py":{"rendered":"context for z.py","thread_count":2,"omitted_thread_count":1}},"summary":{"included_threads":3,"omitted_threads":1,"included_comments":4,"omitted_comments":2,"omitted_marker":"[Review context omitted: 1 thread(s) overall.]"}}`
	context, err := loadReviewContextBody(t, body)
	if err != nil {
		t.Fatalf("loadReviewContext: %v", err)
	}
	if len(context) != 2 {
		t.Fatalf("context path count = %d", len(context))
	}
	if context["src/app.py"] != "context for app.py" {
		t.Fatalf("src/app.py context = %q", context["src/app.py"])
	}
	if context["src/z.py"] != "context for z.py" {
		t.Fatalf("src/z.py context = %q", context["src/z.py"])
	}
}

func TestLoadReviewContextTreatsAbsentAndEmptyAsNoContext(t *testing.T) {
	if context, err := loadReviewContext(""); err != nil || context != nil {
		t.Fatalf("absent context = %#v, %v", context, err)
	}

	context, err := loadReviewContextBody(t, `{"version":1,"paths":{},"summary":{"included_threads":0}}`)
	if err != nil {
		t.Fatalf("loadReviewContext: %v", err)
	}
	if context != nil {
		t.Fatalf("empty context = %#v, want nil", context)
	}
}

func loadReviewContextBody(t *testing.T, body string) (map[string]string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return loadReviewContext(path)
}
