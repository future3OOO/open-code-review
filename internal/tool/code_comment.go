package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/open-code-review/open-code-review/internal/model"
)

var allowedSeverities = map[string]struct{}{
	"critical":     {},
	"high":         {},
	"medium":       {},
	"low":          {},
	"unclassified": {},
}

// CodeCommentProvider submits review comments to the per-Agent CommentCollector.
type CodeCommentProvider struct {
	Collector *CommentCollector
}

func (p *CodeCommentProvider) Tool() Tool { return CodeComment }

func (p *CodeCommentProvider) Execute(_ context.Context, args map[string]any) (string, error) {
	if p.Collector == nil {
		return "Error: comment collector is not configured", nil
	}

	comments, errMsg := ParseComments(args)
	if errMsg != "" {
		return errMsg, nil
	}

	for i := range comments {
		p.Collector.Add(comments[i])
	}
	return CommentSucceed, nil
}

// ParseComments extracts LlmComment entries from tool call arguments without writing
// to the Collector. Returns parsed comments and an error message (empty on success).
func ParseComments(args map[string]any) ([]model.LlmComment, string) {
	var rawComments []any
	if arr, ok := args["comments"].([]any); ok && len(arr) > 0 {
		rawComments = arr
	} else if s, ok := args["comments"].(string); ok && s != "" {
		if err := json.Unmarshal([]byte(s), &rawComments); err != nil {
			return nil, fmt.Sprintf("Error: failed to parse 'comments' JSON string: %v", err)
		}
	}
	if len(rawComments) == 0 {
		raw, _ := json.Marshal(args)
		return nil, fmt.Sprintf("Error: 'comments' array is required. Got args: %s", string(raw))
	}

	var comments []model.LlmComment
	for _, raw := range rawComments {
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, "Error: every comment must be an object"
		}

		cm := model.LlmComment{}

		if content, ok := obj["content"].(string); ok {
			cm.Content = content
		}
		cm.Severity = commentSeverity(obj["severity"])
		if failureMode, ok := obj["failure_mode"].(string); ok {
			cm.FailureMode = failureMode
		}
		if contract, ok := obj["violated_contract"].(string); ok {
			cm.ViolatedContract = contract
		}
		if evidence, ok := obj["evidence"].(string); ok {
			cm.Evidence = evidence
		}
		if suggestion, ok := obj["suggestion_code"].(string); ok {
			cm.SuggestionCode = suggestion
		}
		if existing, ok := obj["existing_code"].(string); ok {
			cm.ExistingCode = existing
		}
		if thinking, ok := obj["thinking"].(string); ok {
			cm.Thinking = thinking
		}
		if path, ok := args["path"].(string); ok {
			cm.Path = path
		}

		if cm.Path == "" || cm.Severity == "" || strings.TrimSpace(cm.Content) == "" ||
			strings.TrimSpace(cm.FailureMode) == "" || strings.TrimSpace(cm.ViolatedContract) == "" ||
			strings.TrimSpace(cm.Evidence) == "" || strings.TrimSpace(cm.ExistingCode) == "" {
			return nil, "Error: missing top-level 'path' argument or invalid comment: every comment requires a valid severity, content, failure_mode, violated_contract, evidence, and existing_code"
		}

		comments = append(comments, cm)
	}
	return comments, ""
}

func commentSeverity(value any) string {
	severity, ok := value.(string)
	if !ok {
		return ""
	}
	severity = strings.ToLower(strings.TrimSpace(severity))
	if _, ok := allowedSeverities[severity]; ok {
		return severity
	}
	return ""
}
