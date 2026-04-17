// Package rules loads system review rules and matches file paths against glob patterns.
package rules

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// SystemRule holds review rules loaded from an external JSON config.
type SystemRule struct {
	DefaultRule string            `json:"default_rule"`
	PathRuleMap map[string]string `json:"path_rule_map"`
}

//go:embed system_rules.json
var defaultSystemRules []byte

// LoadDefault parses the embedded system_rules.json.
func LoadDefault() (*SystemRule, error) {
	var rule SystemRule
	if err := json.Unmarshal(defaultSystemRules, &rule); err != nil {
		return nil, fmt.Errorf("unmarshal default system rules: %w", err)
	}
	return &rule, nil
}

// LoadFile parses a system_rules.json file from disk.
func LoadFile(path string) (*SystemRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rule file %s: %w", path, err)
	}
	var rule SystemRule
	if err := json.Unmarshal(data, &rule); err != nil {
		return nil, fmt.Errorf("unmarshal rule file: %w", err)
	}
	return &rule, nil
}

// Resolve returns the rule text for a given file path.
// Patterns with brace expansion like "*.{go,py}" are expanded into "*.go", "*.py".
// The first match wins; if none match, it falls back to DefaultRule.
// Supports full glob syntax including ** for recursive directory matching.
func (r *SystemRule) Resolve(path string) string {
	for pattern, rule := range r.PathRuleMap {
		expanded := expandBraces(pattern)
		for _, p := range expanded {
			if matched, _ := doublestar.Match(p, path); matched {
				return rule
			}
		}
	}
	return r.DefaultRule
}

// expandBraces turns "{a,b,c}" style patterns into individual strings.
// e.g. "*.go.{java,kotlin}" → ["*.go.java", "*.go.kotlin"].
// If no braces exist, returns the original pattern unchanged.
func expandBraces(s string) []string {
	openIdx := strings.IndexByte(s, '{')
	if openIdx < 0 {
		return []string{s}
	}

	closeIdx := strings.IndexByte(s[openIdx:], '}')
	if closeIdx < 0 {
		return []string{s}
	}
	closeIdx += openIdx

	prefix := s[:openIdx]
	suffix := s[closeIdx+1:]
	options := strings.Split(s[openIdx+1:closeIdx], ",")

	results := make([]string, 0, len(options))
	for _, opt := range options {
		results = append(results, prefix+opt+suffix)
	}
	return results
}
