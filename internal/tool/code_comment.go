package tool

import (
	"encoding/json"
	"fmt"

	"github.com/argus-review/argus/internal/model"
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
	rawComments, ok := args["comments"].([]any)
	if !ok || len(rawComments) == 0 {
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
