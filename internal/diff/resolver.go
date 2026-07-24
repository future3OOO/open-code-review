package diff

import (
	"slices"
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

// ResolveComment attempts to resolve StartLine/EndLine for a single comment
// by matching ExistingCode against the diff. Returns true on success.
func ResolveComment(cm *model.LlmComment, d *model.Diff) bool {
	if cm.StartLine > 0 || cm.EndLine > 0 {
		return true
	}
	if cm.ExistingCode == "" {
		return false
	}
	if resolveFromHunk(d, cm) {
		return true
	}
	return resolveFromFileContent(d, cm)
}

// ResolveRevalidationComment resolves a previously published comment against
// current-file source, using its old line range when the anchor text changed.
func ResolveRevalidationComment(cm *model.LlmComment, d *model.Diff) bool {
	oldStart, oldEnd := cm.StartLine, cm.EndLine
	current := *cm
	current.StartLine = 0
	current.EndLine = 0
	if resolveFromFileContent(d, &current) && setCurrentRange(&current, d, current.StartLine, current.EndLine) {
		*cm = current
		return true
	}

	hunks := ParseHunks(d.Diff)
	start, end, ok := mapOldRange(hunks, oldStart, oldEnd)
	if !ok || !setCurrentRange(&current, d, start, end) {
		return false
	}
	anchor := strings.Join(splitAndNormalize(cm.ExistingCode), "\n")
	currentAnchor := strings.Join(splitAndNormalize(current.ExistingCode), "\n")
	if !strings.Contains(currentAnchor, anchor) &&
		!oldRangeContainsAnchor(hunks, oldStart, oldEnd, anchor) {
		return false
	}
	*cm = current
	return true
}

func mapOldRange(hunks []Hunk, start, end int) (int, int, bool) {
	if start <= 0 || end < start {
		return 0, 0, false
	}
	overlaps := 0
	for i := range hunks {
		hunkEnd := hunks[i].OldStart + hunks[i].OldCount - 1
		if hunks[i].OldCount > 0 && start <= hunkEnd && end >= hunks[i].OldStart {
			overlaps++
		}
	}
	if overlaps > 1 {
		return 0, 0, false
	}
	mappedStart, startOK := mapOldBoundary(hunks, start, false)
	mappedEnd, endOK := mapOldBoundary(hunks, end, true)
	return mappedStart, mappedEnd, startOK && endOK && mappedStart <= mappedEnd
}

func mapOldBoundary(hunks []Hunk, target int, endBoundary bool) (int, bool) {
	shift := 0
	for i := range hunks {
		hunk := &hunks[i]
		if target < hunk.OldStart {
			return target + shift, true
		}
		if target >= hunk.OldStart+hunk.OldCount {
			shift += hunk.NewCount - hunk.OldCount
			continue
		}
		oldLine, newLine := hunk.OldStart, hunk.NewStart
		for lineIndex := 0; lineIndex < len(hunk.Lines); {
			if hunk.Lines[lineIndex].Type == HunkContext {
				if oldLine == target {
					return newLine, true
				}
				oldLine++
				newLine++
				lineIndex++
				continue
			}

			blockNewStart := newLine
			containsTarget := false
			added := 0
			for lineIndex < len(hunk.Lines) && hunk.Lines[lineIndex].Type != HunkContext {
				switch hunk.Lines[lineIndex].Type {
				case HunkAdded:
					added++
					newLine++
				case HunkDeleted:
					containsTarget = containsTarget || oldLine == target
					oldLine++
				}
				lineIndex++
			}
			if containsTarget {
				if added == 0 {
					return 0, false
				}
				if endBoundary {
					return blockNewStart + added - 1, true
				}
				return blockNewStart, true
			}
		}
		return 0, false
	}
	return target + shift, true
}

func oldRangeContainsAnchor(hunks []Hunk, start, end int, anchor string) bool {
	if anchor == "" {
		return false
	}
	var lines []string
	for i := range hunks {
		for _, line := range extractSideLines(&hunks[i], false) {
			if line.lineNum >= start && line.lineNum <= end {
				lines = append(lines, line.content)
			}
		}
	}
	oldRange := strings.Join(splitAndNormalize(strings.Join(lines, "\n")), "\n")
	return oldRange != "" && (strings.Contains(oldRange, anchor) || strings.Contains(anchor, oldRange))
}

func setCurrentRange(cm *model.LlmComment, d *model.Diff, start, end int) bool {
	lines := strings.Split(d.NewFileContent, "\n")
	if start <= 0 || end < start || end > len(lines) {
		return false
	}
	block := lines[start-1 : end]
	anchor := strings.Join(block, "\n")
	if strings.TrimSpace(anchor) == "" {
		return false
	}
	matches := 0
	for i := 0; i+len(block) <= len(lines); i++ {
		if slices.Equal(lines[i:i+len(block)], block) {
			matches++
		}
	}
	if matches != 1 {
		return false
	}
	cm.ExistingCode = anchor
	cm.StartLine = start
	cm.EndLine = end
	return true
}

// indexedLine pairs a normalized line with its absolute file line number.
type indexedLine struct {
	lineNum int
	content string
}

// resolveFromHunk tries to find startLine/endLine by matching ExistingCode
// against hunk lines. It tries the new-side first (context + added lines →
// new-file line numbers), then falls back to old-side (context + deleted →
// old-file line numbers).
func resolveFromHunk(d *model.Diff, cm *model.LlmComment) bool {
	hunks := ParseHunks(d.Diff)
	if len(hunks) == 0 {
		return false
	}

	targetLines := splitAndNormalize(cm.ExistingCode)
	if len(targetLines) == 0 {
		return false
	}

	for i := range hunks {
		newSide := extractSideLines(&hunks[i], true)
		if start, end, ok := matchConsecutive(newSide, targetLines); ok {
			cm.StartLine = start
			cm.EndLine = end
			return true
		}
	}

	for i := range hunks {
		oldSide := extractSideLines(&hunks[i], false)
		if start, end, ok := matchConsecutive(oldSide, targetLines); ok {
			cm.StartLine = start
			cm.EndLine = end
			return true
		}
	}

	return false
}

// extractSideLines extracts one side of the diff from a hunk.
// When newSide is true, returns context+added lines with new-file line numbers.
// When newSide is false, returns context+deleted lines with old-file line numbers.
func extractSideLines(hunk *Hunk, newSide bool) []indexedLine {
	var result []indexedLine
	oldLine := hunk.OldStart
	newLine := hunk.NewStart

	for _, l := range hunk.Lines {
		switch l.Type {
		case HunkContext:
			if newSide {
				result = append(result, indexedLine{newLine, normalizeLine(l.Content)})
			} else {
				result = append(result, indexedLine{oldLine, normalizeLine(l.Content)})
			}
			oldLine++
			newLine++
		case HunkAdded:
			if newSide {
				result = append(result, indexedLine{newLine, normalizeLine(l.Content)})
			}
			newLine++
		case HunkDeleted:
			if !newSide {
				result = append(result, indexedLine{oldLine, normalizeLine(l.Content)})
			}
			oldLine++
		}
	}
	return result
}

// matchConsecutive scans sideLines for a consecutive run matching all targetLines.
func matchConsecutive(sideLines []indexedLine, targetLines []string) (startLine, endLine int, found bool) {
	if len(targetLines) == 0 || len(sideLines) < len(targetLines) {
		return 0, 0, false
	}
	for i := 0; i <= len(sideLines)-len(targetLines); i++ {
		matched := true
		for j, target := range targetLines {
			if sideLines[i+j].content != target {
				matched = false
				break
			}
		}
		if matched {
			return sideLines[i].lineNum, sideLines[i+len(targetLines)-1].lineNum, true
		}
	}
	return 0, 0, false
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
