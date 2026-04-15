package config

import "fmt"

// Template holds the native agent task template configuration.
// Mirrors NativeAgentTemplate from the Java implementation, loaded via YAML/JSON at runtime.
type Template struct {
	MainTask                   LlmConversation `yaml:"main_task" json:"main_task"`
	PlanTask                   *LlmConversation `yaml:"plan_task,omitempty" json:"plan_task,omitempty"`
	MemoryCompressionTask      LlmConversation `yaml:"memory_compression_task" json:"memory_compression_task"`
	CodeReviewBackgroundTpl   string            `yaml:"code_review_background_template" json:"code_review_background_template"`
	TokenWarningThreshold     int               `yaml:"token_warning_threshold" json:"token_warning_threshold"`
	ToolRequestWaitTimeMs     int               `yaml:"tool_request_wait_time_ms" json:"tool_request_wait_time_ms"`
	MaxToolRequestTimes       int               `yaml:"max_tool_request_times" json:"max_tool_request_times"`
	MaxSubtaskExecutionMinutes int              `yaml:"max_subtask_execution_time_minutes" json:"max_subtask_execution_time_minutes"`
}

// Validate checks required template fields.
func (t *Template) Validate() error {
	if t.TokenWarningThreshold <= 0 {
		return fmt.Errorf("token_warning_threshold must be positive")
	}
	if t.MaxToolRequestTimes <= 0 {
		return fmt.Errorf("max_tool_request_times must be positive")
	}
	if t.MainTask.Model == "" {
		return fmt.Errorf("main_task.model is required")
	}
	return nil
}

// LlmConversation mirrors LlmConversation from the Java side — a preset prompt with model settings.
type LlmConversation struct {
	Model   string             `yaml:"model" json:"model"`
	Timeout int                `yaml:"timeout" json:"timeout"`
	Messages []ChatMessage    `yaml:"messages" json:"messages"`
}

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	Role    string `yaml:"role" json:"role"`
	Content string `yaml:"content" json:"content"`
}
