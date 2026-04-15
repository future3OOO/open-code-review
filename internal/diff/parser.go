// Package diff parses unified git diff output into structured Diff objects.
package diff

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/argus-review/argus/internal/model"
)

var (
	diffHeaderRe = regexp.MustCompile(`^diff --git a/(.+?) b/(.+)$`)
	oldFileRe    = regexp.MustCompile(`^--- a/(.+)$`)
	newFileRe    = regexp.MustCompile(`^\+\+\+ b/(.+)$`)
	binaryRe     = regexp.MustCompile(`Binary files `)
)

// ParseDiffText splits the unified diff text into per-file Diff structs.
func ParseDiffText(diffText string, repoDir string) ([]model.Diff, error) {
	lines := strings.Split(diffText, "\n")
	var diffs []model.Diff
	var current *model.Diff
	var buf strings.Builder

	for _, line := range lines {
		if m := diffHeaderRe.FindStringSubmatch(line); m != nil {
			// Flush previous diff
			if current != nil {
				current.Diff = strings.TrimSuffix(buf.String(), "\n")
				finalizeDiff(current, repoDir)
				diffs = append(diffs, *current)
				buf.Reset()
			}
			current = &model.Diff{
				OldPath: m[1],
				NewPath: m[2],
			}
		}
		if current == nil {
			continue
		}

		switch {
		case binaryRe.MatchString(line):
			current.IsBinary = true
		case oldFileRe.MatchString(line):
			if p := oldFileRe.FindStringSubmatch(line); len(p) > 1 && p[1] == "/dev/null" {
				current.IsNew = true
			}
		case newFileRe.MatchString(line):
			if p := newFileRe.FindStringSubmatch(line); len(p) > 1 && p[1] == "/dev/null" {
				current.IsDeleted = true
			}
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			current.Insertions++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			current.Deletions++
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}

	// Flush last diff
	if current != nil {
		current.Diff = strings.TrimSuffix(buf.String(), "\n")
		finalizeDiff(current, repoDir)
		diffs = append(diffs, *current)
	}

	return diffs, nil
}

// finalizeDiff attempts to read the new file content from disk.
func finalizeDiff(d *model.Diff, repoDir string) {
	if d.IsDeleted || d.NewPath == "/dev/null" {
		d.NewPath = "/dev/null"
		return
	}
	fullPath := filepath.Join(repoDir, d.NewPath)
	content, err := os.ReadFile(fullPath)
	if err == nil {
		d.NewFileContent = string(content)
	}
}
