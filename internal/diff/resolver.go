package diff

import (
	"strings"

	"github.com/open-code-review/open-code-review/internal/model"
)

// ResolveLineNumbers populates StartLine/EndLine on each comment by matching
// the ExistingCode against the corresponding file's diff hunks (primary), or
// falling back to scanning the full new-file content line-by-line.
func ResolveLineNumbers(comments []model.LlmComment, diffs []model.Diff) []model.LlmComment {
	if len(comments) == 0 || len(diffs) == 0 {
		return comments
	}

	// Build lookup: newPath -> *Diff
	diffByPath := make(map[string]*model.Diff, len(diffs))
	for i := range diffs {
		d := &diffs[i]
		if d.NewPath != "/dev/null" && d.NewPath != "" {
			diffByPath[d.NewPath] = d
		}
		if d.OldPath != "/dev/null" && d.OldPath != "" {
			diffByPath[d.OldPath] = d
		}
	}

	result := make([]model.LlmComment, len(comments))
	copy(result, comments)

	for i := range result {
		cm := &result[i]
		if cm.StartLine > 0 || cm.EndLine > 0 {
			continue
		}
		if cm.ExistingCode == "" {
			continue
		}
		d, ok := diffByPath[cm.Path]
		if !ok {
			continue
		}

		// Primary: try matching from deleted/context lines in diff hunks
		if resolveFromHunk(d, cm) {
			continue
		}

		// Fallback: scan the new file content for consecutive matches
		resolveFromFileContent(d, cm)
	}

	return result
}

// resolveFromHunk tries to find the startLine/endLine by matching ExistingCode
// against "from" side lines (context + deleted) in the diff hunks.
// Returns true on success (comments fields are mutated in place).
func resolveFromHunk(d *model.Diff, cm *model.LlmComment) bool {
	// TODO: re-track with llm
	hunks := ParseHunks(d.Diff)
	if len(hunks) == 0 {
		return false
	}

	targetLines := splitAndNormalize(cm.ExistingCode)
	if len(targetLines) == 0 {
		return false
	}

	for i := range hunks {
		hunk := &hunks[i]
		offset := 0 // tracks position within old-file range

		lines := hunk.Lines
		for j := 0; j < len(lines); j++ {
			line := lines[j]

			switch line.Type {
			case HunkAdded:
				// Added lines are TO-side only; don't affect old-file offset
				continue
			case HunkContext, HunkDeleted:
				// Both are FROM-side candidates
				if matchAt(lines, j, targetLines) {
					startLine := hunk.OldStart + offset
					endLine := startLine + len(targetLines) - 1
					cm.StartLine = startLine
					cm.EndLine = endLine
					return true
				}
				offset++
			}
		}
	}

	return false
}

// matchAt checks whether targetLines[i] matches the normalized content of
// lines[startIndex+i] for all i, where each matched line must be a "from" side
// line (context or deleted).
func matchAt(lines []HunkLine, startIndex int, targetLines []string) bool {
	for i, target := range targetLines {
		idx := startIndex + i
		if idx >= len(lines) {
			return false
		}
		l := lines[idx]
		// Must be a "from" side line
		if l.Type != HunkContext && l.Type != HunkDeleted {
			return false
		}
		if normalizeLine(l.Content) != target {
			return false
		}
	}
	return true
}

// resolveFromFileContent scans the new file content line-by-line for consecutive
// matches of the normalized existing_code. Ported from Java's findConsecutiveLines.
func resolveFromFileContent(d *model.Diff, cm *model.LlmComment) bool {
	if d.NewFileContent == "" {
		return false
	}

	fileLines := strings.Split(d.NewFileContent, "\n")
	targetLines := splitAndNormalize(cm.ExistingCode)
	if len(targetLines) == 0 || len(fileLines) < len(targetLines) {
		return false
	}

	for i := 0; i <= len(fileLines)-len(targetLines); i++ {
		matched := true
		for j, target := range targetLines {
			if normalizeLine(strings.TrimRight(fileLines[i+j], "\r")) != target {
				matched = false
				break
			}
		}
		if matched {
			cm.StartLine = i + 1
			cm.EndLine = i + len(targetLines)
			return true
		}
	}

	return false
}

// splitAndNormalize splits code text into lines and normalizes each one.
func splitAndNormalize(code string) []string {
	raw := strings.Split(code, "\n")
	result := make([]string, 0, len(raw))
	for _, line := range raw {
		n := normalizeLine(line)
		if n == "" {
			continue
		}
		result = append(result, n)
	}
	return result
}

// normalizeLine removes leading/trailing whitespace and strips any leading
// '+' or '-' diff marker (mirrors Java's processTargetLineCode).
func normalizeLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "+")
	s = strings.TrimPrefix(s, "-")
	return strings.TrimSpace(s)
}
