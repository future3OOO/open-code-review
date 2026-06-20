package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/open-code-review/open-code-review/internal/config/rules"
	"github.com/open-code-review/open-code-review/internal/llm"
	"github.com/open-code-review/open-code-review/internal/model"
	"github.com/open-code-review/open-code-review/internal/session"
	"github.com/open-code-review/open-code-review/internal/stdout"
	"github.com/open-code-review/open-code-review/internal/telemetry"
)

// DiffGroup represents a set of functionally related diffs to be reviewed together.
type DiffGroup struct {
	Diffs  []model.Diff
	Reason string
}

type groupingResponse struct {
	Files  []string `json:"files"`
	Reason string   `json:"reason"`
}

// executeFileGrouping groups diffs for co-review using LLM-driven clustering.
func (a *Agent) executeFileGrouping(ctx context.Context, diffs []model.Diff) []DiffGroup {
	threshold := a.args.Template.GroupingLineThreshold
	if threshold <= 0 {
		threshold = 1000
	}

	if a.args.Template.FileGroupingTask == nil || len(diffs) < 2 {
		return perFileFallback(diffs)
	}

	var totalLines int64
	for _, d := range diffs {
		totalLines += d.Insertions + d.Deletions
	}
	if totalLines < int64(threshold/2) {
		fmt.Fprintf(stdout.Writer(), "[ocr] Skipping grouping LLM (%d lines < %d), treating all files as one group\n", totalLines, threshold/2)
		return []DiffGroup{{Diffs: diffs, Reason: "total changes below grouping threshold"}}
	}

	var largeDiffs []model.Diff
	var normalDiffs []model.Diff
	for _, d := range diffs {
		if d.Insertions+d.Deletions > int64(threshold) {
			largeDiffs = append(largeDiffs, d)
		} else {
			normalDiffs = append(normalDiffs, d)
		}
	}

	if len(normalDiffs) < 2 {
		return perFileFallback(diffs)
	}

	resp, err := a.callGroupingLLM(ctx, normalDiffs)
	if err != nil {
		fmt.Fprintf(stdout.Writer(), "[ocr] File grouping LLM failed: %v (falling back to per-file mode)\n", err)
		return perFileFallback(diffs)
	}

	groups := validateAndBuildGroups(resp, normalDiffs)
	groups = a.splitOverLimitGroups(ctx, groups, threshold, 0)

	for _, d := range largeDiffs {
		groups = append(groups, DiffGroup{Diffs: []model.Diff{d}, Reason: "exceeds grouping threshold"})
	}

	return groups
}

// callGroupingLLM invokes the LLM to produce file groupings.
func (a *Agent) callGroupingLLM(ctx context.Context, diffs []model.Diff) ([]groupingResponse, error) {
	tpl := a.args.Template.FileGroupingTask
	if tpl == nil || len(tpl.Messages) == 0 {
		return nil, fmt.Errorf("file_grouping_task template not configured")
	}

	fileList := buildGroupingFileList(diffs)
	threshold := a.args.Template.GroupingLineThreshold
	if threshold <= 0 {
		threshold = 1000
	}

	messages := make([]llm.Message, 0, len(tpl.Messages))
	for _, m := range tpl.Messages {
		content := m.Content
		content = strings.ReplaceAll(content, "{{file_list}}", fileList)
		content = strings.ReplaceAll(content, "{{grouping_line_threshold}}", strconv.Itoa(threshold))
		messages = append(messages, llm.NewTextMessage(m.Role, content))
	}

	fs := a.session.GetOrCreateFileSession("__file_grouping__")
	rec := fs.AppendTaskRecord(session.FileGroupingTask, append([]llm.Message(nil), messages...))
	startTime := time.Now()

	resp, err := a.args.LLMClient.CompletionsWithCtx(ctx, llm.ChatRequest{
		Model:     a.args.Model,
		Messages:  messages,
		MaxTokens: 4096,
	})
	duration := time.Since(startTime)
	if err != nil {
		rec.SetError(err, duration)
		telemetry.RecordLLMRequest(ctx, a.args.Model, duration, 0, "error")
		return nil, fmt.Errorf("grouping LLM call: %w", err)
	}
	rec.SetResponse(resp, duration)
	totalTokens := int64(0)
	if resp.Usage != nil {
		totalTokens = resp.Usage.TotalTokens
	}
	telemetry.RecordLLMRequest(ctx, a.args.Model, duration, totalTokens, "ok")

	if resp.Usage != nil {
		a.addTokenUsage(resp.Usage)
	}

	text := stripMarkdownFences(resp.Content())
	var groups []groupingResponse
	if err := json.Unmarshal([]byte(text), &groups); err != nil {
		if extracted := extractJSONArray(text); extracted != "" {
			if err2 := json.Unmarshal([]byte(extracted), &groups); err2 == nil {
				return groups, nil
			}
		}
		return nil, fmt.Errorf("parse grouping response: %w (raw: %s)", err, truncate(text, 200))
	}
	return groups, nil
}

// validateAndBuildGroups maps LLM response back to actual diffs, handling unknown paths and missing files.
func validateAndBuildGroups(resp []groupingResponse, diffs []model.Diff) []DiffGroup {
	diffMap := make(map[string]model.Diff, len(diffs))
	assigned := make(map[string]bool, len(diffs))
	for _, d := range diffs {
		diffMap[effectivePath(d)] = d
	}

	var groups []DiffGroup
	for _, g := range resp {
		var matched []model.Diff
		for _, f := range g.Files {
			if d, ok := diffMap[f]; ok && !assigned[f] {
				matched = append(matched, d)
				assigned[f] = true
			}
		}
		if len(matched) > 0 {
			groups = append(groups, DiffGroup{Diffs: matched, Reason: g.Reason})
		}
	}

	for _, d := range diffs {
		if !assigned[effectivePath(d)] {
			groups = append(groups, DiffGroup{Diffs: []model.Diff{d}, Reason: "unassigned by grouping"})
		}
	}
	return groups
}

// splitOverLimitGroups recursively splits groups that exceed the line threshold.
func (a *Agent) splitOverLimitGroups(ctx context.Context, groups []DiffGroup, threshold int, depth int) []DiffGroup {
	const maxDepth = 1

	var result []DiffGroup
	for i, g := range groups {
		select {
		case <-ctx.Done():
			result = append(result, groups[i:]...)
			return result
		default:
		}

		if len(g.Diffs) <= 1 || groupTotalLines(g) <= int64(threshold) {
			result = append(result, g)
			continue
		}

		if depth >= maxDepth {
			for _, d := range g.Diffs {
				result = append(result, DiffGroup{Diffs: []model.Diff{d}, Reason: "split depth exceeded"})
			}
			continue
		}

		subResp, err := a.callGroupingLLM(ctx, g.Diffs)
		if err != nil {
			for _, d := range g.Diffs {
				result = append(result, DiffGroup{Diffs: []model.Diff{d}, Reason: "re-group failed"})
			}
			continue
		}

		subGroups := validateAndBuildGroups(subResp, g.Diffs)
		result = append(result, a.splitOverLimitGroups(ctx, subGroups, threshold, depth+1)...)
	}
	return result
}

// perFileFallback returns each diff as its own group.
func perFileFallback(diffs []model.Diff) []DiffGroup {
	groups := make([]DiffGroup, len(diffs))
	for i, d := range diffs {
		groups[i] = DiffGroup{Diffs: []model.Diff{d}, Reason: "per-file"}
	}
	return groups
}

// buildGroupingFileList formats diffs as "path  +insertions/-deletions" lines.
func buildGroupingFileList(diffs []model.Diff) string {
	var sb strings.Builder
	for i, d := range diffs {
		sb.WriteString(fmt.Sprintf("%s  +%d/-%d", d.NewPath, d.Insertions, d.Deletions))
		if i < len(diffs)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// groupTotalLines returns the total insertions + deletions across a group.
func groupTotalLines(g DiffGroup) int64 {
	var total int64
	for _, d := range g.Diffs {
		total += d.Insertions + d.Deletions
	}
	return total
}

// resolveGroupRule resolves and merges system rules for a group of file paths.
// If all files match the same rule text, it returns that single rule.
// If files match different rules, it returns them concatenated with === pattern === headers.
func (a *Agent) resolveGroupRule(paths []string) string {
	if a.args.SystemRule == nil || len(paths) == 0 {
		return ""
	}

	type patternRule struct {
		Pattern string
		Rule    string
	}

	var collected []patternRule
	seenRules := make(map[string]bool)

	dr, hasDetail := a.args.SystemRule.(rules.DetailResolver)

	for _, p := range paths {
		lp := strings.ToLower(p)
		var pattern, ruleText string
		if hasDetail {
			detail := dr.ResolveDetail(lp)
			pattern = detail.Pattern
			ruleText = detail.Rule
		} else {
			ruleText = a.args.SystemRule.Resolve(lp)
			pattern = lp
		}

		if ruleText == "" {
			continue
		}
		key := pattern + "\x00" + ruleText
		if seenRules[key] {
			continue
		}
		seenRules[key] = true
		collected = append(collected, patternRule{Pattern: pattern, Rule: ruleText})
	}

	if len(collected) == 0 {
		return ""
	}

	allSame := true
	for i := 1; i < len(collected); i++ {
		if collected[i].Rule != collected[0].Rule {
			allSame = false
			break
		}
	}

	if allSame {
		return collected[0].Rule
	}

	var sb strings.Builder
	for i, pr := range collected {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("=== ")
		sb.WriteString(pr.Pattern)
		sb.WriteString(" ===\n")
		sb.WriteString(pr.Rule)
	}
	return sb.String()
}

// addTokenUsage atomically accumulates token usage from an LLM response.
func (a *Agent) addTokenUsage(usage *llm.UsageInfo) {
	if usage == nil {
		return
	}
	atomic.AddInt64(&a.totalInputTokens, usage.PromptTokens)
	atomic.AddInt64(&a.totalOutputTokens, usage.CompletionTokens)
	atomic.AddInt64(&a.totalCacheReadTokens, usage.CacheReadTokens)
	atomic.AddInt64(&a.totalCacheWriteTokens, usage.CacheWriteTokens)
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
