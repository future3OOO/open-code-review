package tool

import (
	"encoding/json"
	"fmt"

	"github.com/open-code-review/open-code-review/internal/model"
)

// CodeCommentProvider submits review comments to the per-Agent CommentCollector.
type CodeCommentProvider struct {
	Collector *CommentCollector
}

func (p *CodeCommentProvider) Tool() Tool { return CodeComment }

func (p *CodeCommentProvider) Execute(args map[string]any) (string, error) {
	if p.Collector == nil {
		return "Error: comment collector is not configured", nil
	}

	// Parse the "comments" array from the tool call arguments.
	// LLM sometimes double-encodes the array as a JSON string.
	var rawComments []any
	if arr, ok := args["comments"].([]any); ok && len(arr) > 0 {
		rawComments = arr
	} else if s, ok := args["comments"].(string); ok && s != "" {
		_ = json.Unmarshal([]byte(s), &rawComments)
	}
	if len(rawComments) == 0 {
		raw, _ := json.Marshal(args)
		return fmt.Sprintf("Error: 'comments' array is required. Got args: %s", string(raw)), nil
	}

	for _, raw := range rawComments {
		obj, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		cm := model.LlmComment{}

		if content, ok := obj["content"].(string); ok {
			cm.Content = content
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

		if cm.Path == "" || cm.Content == "" {
			continue
		}

		p.Collector.Add(cm)
	}
	return CommentSucceed, nil
}
