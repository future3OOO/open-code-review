package rules

import (
	"testing"

	"github.com/bmatcuk/doublestar/v4"
)

func TestResolve_BareFilename(t *testing.T) {
	rule, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}

	tests := []struct {
		filename string
	}{
		{"main.go"},        // should match *.{go,...} rule
		{"Foo.java"},       // should match *.java rule
		{"config.yaml"},    // should match *.{yaml,yml} rule
		{"App.tsx"},        // should match *.{ts,js,tsx,jsx} rule
		{"pom.xml"},        // should match pom.xml exact filename
		{"app.properties"}, // should match *.properties
		{"data.json"},      // should match *.json
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			resolved := rule.Resolve(tt.filename)
			if resolved == "" {
				t.Fatalf("Resolve(%q) returned empty string", tt.filename)
			}
			if resolved == rule.DefaultRule {
				t.Fatalf("Resolve(%q) returned DefaultRule instead of the specific rule\nDefaultRule: %s\nResolved: %.100s",
					tt.filename, rule.DefaultRule, resolved)
			}
		})
	}
}

func TestDoublestarMatch_GlobSyntax(t *testing.T) {
	// Tests demonstrating doublestar.Match capabilities with full glob syntax.
	// "**" matches zero or more directories (crosses "/" separators).
	// Users can configure patterns like "**/*.go" in path_rule_map.
	globTests := []struct {
		pattern     string
		path        string
		expectMatch bool
	}{
		{"**/*.go", "internal/agent/agent.go", true},
		{"**/*.go", "agent.go", true},
		{"**/*.go", "a/b/c/d/main.go", true},
		{"**/*.java", "src/main/java/com/Foo.java", true},
		{"**/*.{ts,tsx}", "frontend/src/App.tsx", true},
		{"**/*.{yaml,yml}", "config/app.yaml", true},
		{"**/pom.xml", "module/pom.xml", true},
		{"**/pom.xml", "pom.xml", true},
		{"**/*mapper*.xml", "src/main/resources/mapper/usermapper.xml", true}, // outer caller lowercases path first, then matches
	}

	for _, tt := range globTests {
		t.Run(tt.pattern+" vs "+tt.path, func(t *testing.T) {
			matched, err := doublestar.Match(tt.pattern, tt.path)
			if err != nil {
				t.Fatalf("doublestar.Match error: %v", err)
			}
			if matched != tt.expectMatch {
				t.Errorf("doublestar.Match(%q, %q) = %v, want %v",
					tt.pattern, tt.path, matched, tt.expectMatch)
			}
		})
	}
}

func TestResolve_CurrentDefaults_WithPath(t *testing.T) {
	rule, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}

	// With current default config, patterns like "*.go" do NOT match paths with "/"
	// because "*" doesn't cross directory separators in glob semantics either.
	// This documents the expected behavior — to match subdirectories, use "**/*.ext".
	pathTests := []struct {
		path         string
		wantSpecific bool // true = should match a specific rule (not default)
	}{
		{"internal/agent/agent.go", false},   // "*.go" can't cross "/", falls to default
		{"src/main/java/com/Foo.java", false}, // same reason
		{"frontend/src/App.tsx", false},      // same reason
		{"config/app.yaml", false},           // same reason
		{"pom.xml", true},                    // exact name match works
		{"build.gradle", true},               // exact name match works
		{"agent.go", true},                   // bare filename, "*.go" matches
		{"Foo.java", true},                   // bare filename, "*.java" matches
		{"app.properties", true},             // bare filename
		{"data.json", true},                  // bare filename
	}

	for _, tt := range pathTests {
		resolved := rule.Resolve(tt.path)
		isDefault := resolved == rule.DefaultRule

		if tt.wantSpecific && isDefault {
			t.Errorf("%s: expected specific rule, but got DefaultRule", tt.path)
		}
		if !tt.wantSpecific && !isDefault {
			t.Errorf("%s: expected DefaultRule (pattern lacks **), but got: %.100s", tt.path, resolved)
		}
	}
}

func TestResolve_MapIterationOrder(t *testing.T) {
	rule, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}

	// Run multiple times to check if map iteration randomness causes inconsistent results.
	// For example, "package.json" could be matched by both "package.json" (exact) and "*.json".
	// Which rule wins depends on which pattern the map yields first - this is non-deterministic.
	results := make(map[string]int)

	for i := 0; i < 100; i++ {
		resolved := rule.Resolve("package.json")
		results[resolved]++
	}

	// If there are more than 1 unique results, map iteration order affects outcome.
	if len(results) > 1 {
		t.Errorf("Resolve('package.json') returned %d different results across 100 iterations:\nmap iteration order is non-deterministic", len(results))
	}
}

func TestExpandBraces(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"*.{go,py}", []string{"*.go", "*.py"}},
		{"*.{yaml,yml}", []string{"*.yaml", "*.yml"}},
		{"*.{kt}", []string{"*.kt"}},
		{"pom.xml", []string{"pom.xml"}},
		{"*.java", []string{"*.java"}},
		{"{a,b,c}", []string{"a", "b", "c"}},
		{"unmatched{brace", []string{"unmatched{brace"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := expandBraces(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("expandBraces(%q) returned %d items, want %d", tt.input, len(got), len(tt.expected))
			}
			for i := range tt.expected {
				if got[i] != tt.expected[i] {
					t.Errorf("expandBraces(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
				}
			}
		})
	}
}
