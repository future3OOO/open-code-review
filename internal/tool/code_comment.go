package tool

import (
	"encoding/json"
	"fmt"
	"strconv"

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

	cm := model.LlmComment{}

	if path, ok := args["path"].(string); ok {
		cm.Path = path
	}
	if content, ok := args["content"].(string); ok {
		cm.Content = content
	}
	if suggestion, ok := args["suggestion_code"].(string); ok {
		cm.SuggestionCode = suggestion
	}
	if thinking, ok := args["thinking"].(string); ok {
		cm.Thinking = thinking
	}

	if startLine, ok := args["start_line"]; ok {
		switch v := startLine.(type) {
		case float64:
			cm.StartLine = int(v)
		case string:
			if n, err := strconv.Atoi(v); err == nil {
				cm.StartLine = n
			}
		}
	}
	if endLine, ok := args["end_line"]; ok {
		switch v := endLine.(type) {
		case float64:
			cm.EndLine = int(v)
		case string:
			if n, err := strconv.Atoi(v); err == nil {
				cm.EndLine = n
			}
		}
	}

	if cm.Path == "" || cm.Content == "" {
		raw, _ := json.Marshal(args)
		return fmt.Sprintf("Error: path and content are required. Got args: %s", string(raw)), nil
	}

	p.Collector.Add(cm)
	return CommentSucceed, nil
}
