package rules

import (
	"strings"
	"testing"
)

func TestExpandBraces_NoBraces(t *testing.T) {
	got := expandBraces("*.java")
	if len(got) != 1 || got[0] != "*.java" {
		t.Errorf("expected [*.java], got %v", got)
	}
}

func TestExpandBraces_SingleGroup(t *testing.T) {
	got := expandBraces("*.{go,py}")
	want := []string{"*.go", "*.py"}
	if len(got) != len(want) {
		t.Fatalf("expected %d items, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestExpandBraces_MultipleOptions(t *testing.T) {
	got := expandBraces("**/*.{ts,js,tsx,jsx}")
	want := []string{"**/*.ts", "**/*.js", "**/*.tsx", "**/*.jsx"}
	if len(got) != len(want) {
		t.Fatalf("expected %d items, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestExpandBraces_UnclosedBrace(t *testing.T) {
	got := expandBraces("*.{go,py")
	if len(got) != 1 || got[0] != "*.{go,py" {
		t.Errorf("expected original pattern, got %v", got)
	}
}

func TestResolve_DefaultRules(t *testing.T) {
	rule, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}

	tests := []struct {
		path       string
		wantSubstr string // substring that should appear in the matched rule
	}{
		{"src/main/java/com/example/foo.java", "逻辑错误识别"},
		{"foo.java", "逻辑错误识别"},
		{"internal/agent/agent.go", "逻辑问题"},
		{"scripts/deploy.py", "逻辑问题"},
		{"src/main/resources/mapper/usermapper.xml", "SQL逻辑错误识别"},
		{"src/main/resources/dao/userdao.xml", "SQL逻辑错误识别"},
		{"pom.xml", "snapshot"},
		{"submodule/pom.xml", "snapshot"},
		{"src/main/resources/application.properties", "配置错误识别"},
		{"frontend/package.json", "latest"},
		{"config/app.yaml", "yaml-key"},
		{"deploy/values.yml", "yaml-key"},
		{"src/components/app.tsx", "React"},
		{"lib/utils.ts", "TypeScript"},
		{"app.kt", "空安全"},
		{"src/main/handler.cpp", "智能指针"},
		{"driver.c", "malloc"},
		{"ios/ViewController.m", "数组越界"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := rule.Resolve(tt.path)
			if !strings.Contains(got, tt.wantSubstr) {
				t.Errorf("Resolve(%q): expected rule containing %q, got %q",
					tt.path, tt.wantSubstr, truncate(got, 80))
			}
		})
	}
}

func TestResolve_FallbackToDefault(t *testing.T) {
	rule, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}

	paths := []string{
		"readme.md",
		"docs/architecture.txt",
		"Makefile",
		"src/unknown.rs",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			got := rule.Resolve(path)
			if got != rule.DefaultRule {
				t.Errorf("Resolve(%q): expected DefaultRule, got %q", path, truncate(got, 80))
			}
		})
	}
}

func TestResolve_CustomRule_FirstMatchWins(t *testing.T) {
	rule := &SystemRule{
		DefaultRule: "default",
		PathRules: []PathRule{
			{Pattern: "**/special.java", Rule: "special-rule"},
			{Pattern: "**/*.java", Rule: "java-rule"},
		},
	}

	// special.java matches both patterns, but "special-rule" is first.
	got := rule.Resolve("src/special.java")
	if got != "special-rule" {
		t.Errorf("expected special-rule, got %q", got)
	}

	// Other java files match the second pattern.
	got = rule.Resolve("src/foo.java")
	if got != "java-rule" {
		t.Errorf("expected java-rule, got %q", got)
	}
}

func TestResolve_CustomRule_DefaultFallback(t *testing.T) {
	rule := &SystemRule{
		DefaultRule: "fallback-rule",
		PathRules: []PathRule{
			{Pattern: "**/*.java", Rule: "java-rule"},
		},
	}

	got := rule.Resolve("main.go")
	if got != "fallback-rule" {
		t.Errorf("expected fallback-rule, got %q", got)
	}
}

func TestResolve_CaseSensitivity(t *testing.T) {
	rule := &SystemRule{
		DefaultRule: "default",
		PathRules: []PathRule{
			{Pattern: "**/*.java", Rule: "java-rule"},
		},
	}

	// agent.go calls strings.ToLower(newPath) before Resolve,
	// so uppercase extensions should NOT match if not lowercased.
	got := rule.Resolve("Foo.Java")
	if got != "default" {
		t.Errorf("expected default for uppercase extension, got %q", got)
	}

	got = rule.Resolve("foo.java")
	if got != "java-rule" {
		t.Errorf("expected java-rule for lowercase, got %q", got)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
