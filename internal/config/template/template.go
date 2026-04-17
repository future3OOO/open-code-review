// Package template loads and validates task prompt templates for the code review agent.
package template

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// Template holds the native agent task template configuration.
// Mirrors NativeAgentTemplate from the Java implementation, loaded via JSON at runtime.
type Template struct {
	MainTask              LlmConversation  `json:"MAIN_TASK"`
	PlanTask              *LlmConversation `json:"PLAN_TASK,omitempty"`
	MemoryCompressionTask LlmConversation  `json:"MEMORY_COMPRESSION_TASK"`
	TokenWarningThreshold int              `json:"TOKEN_WARNING_THRESHOLD"`
	ToolRequestWaitTimeMs int              `json:"TOOL_REQUEST_WAIT_TIME_MS"`
	MaxToolRequestTimes   int              `json:"MAX_TOOL_REQUEST_TIMES"`
	MaxSubtaskExecMinutes int              `json:"MAX_SUBTASK_EXECUTION_TIME_MINUTES"`
}

//go:embed task_template.json
var defaultTemplate []byte

// LoadDefault parses the embedded task_template.json.
func LoadDefault() (*Template, error) {
	var tpl Template
	if err := json.Unmarshal(defaultTemplate, &tpl); err != nil {
		return nil, fmt.Errorf("unmarshal default template: %w", err)
	}
	return &tpl, nil
}

// Validate checks required template fields.
func (t *Template) Validate() error {
	if t.TokenWarningThreshold <= 0 {
		return fmt.Errorf("token_warning_threshold must be positive")
	}
	if t.MaxToolRequestTimes <= 0 {
		return fmt.Errorf("max_tool_request_times must be positive")
	}
	if len(t.MainTask.Messages) == 0 {
		return fmt.Errorf("main_task.messages must not be empty")
	}
	return nil
}

// LlmConversation mirrors LlmConversation from the Java side — a preset prompt with settings.
type LlmConversation struct {
	Timeout  int           `json:"timeout"`
	Messages []ChatMessage `json:"messages"`
}

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
