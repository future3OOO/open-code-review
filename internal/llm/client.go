// Package llm provides an OpenAI-compatible LLM client interface.
// Argus supports any service that implements the OpenAI Chat Completion API schema,
// including OpenAI, Claude (via Anthropic's OpenAI-compatible endpoint), local models, etc.
package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Message represents a single message in a chat conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Usage holds token usage statistics from a response.
type Usage struct {
	PromptTokens            int64 `json:"prompt_tokens"`
	CompletionTokens        int64 `json:"completion_tokens"`
	CacheCreationInputToken int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens    int64 `json:"cache_read_input_tokens,omitempty"`
}

// Choice holds a single choice from the response.
type Choice struct {
	Message          ResponseMessage `json:"message"`
	FinishReason     string          `json:"finish_reason"`
}

// ToolCall represents a function call requested by the model.
type ToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function FunctionCall   `json:"function"`
}

// FunctionCall holds the name and arguments of a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded string
}

// ResponseMessage extends Message with optional reasoning content.
type ResponseMessage struct {
	Role             string     `json:"role"`
	Content          *string    `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

// ChatResponse is the parsed result of a completion request.
type ChatResponse struct {
	ID      string           `json:"-"`
	Model   string           `json:"-"`
	Choices []Choice         `json:"-"`
	Usage   *Usage           `json:"-"`
	Headers http.Header      `json:"-"` // Raw response headers (may contain session IDs, etc.)
}

// Content extracts the text content from the first choice, falling back to reasoning content.
func (r *ChatResponse) Content() string {
	if len(r.Choices) == 0 {
		return ""
	}
	msg := r.Choices[0].Message
	if msg.Content != nil && *msg.Content != "" {
		cleaned := strings.ReplaceAll(strings.ReplaceAll(*msg.Content, "</think>", ""), "<think>", "")
		return strings.TrimSpace(cleaned)
	}
	return msg.ReasoningContent
}

// ToolCalls extracts tool calls from the first choice.
func (r *ChatResponse) ToolCalls() []ToolCall {
	if len(r.Choices) == 0 {
		return nil
	}
	return r.Choices[0].Message.ToolCalls
}

// ToolDef defines a tool/function available to the model.
type ToolDef struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef specifies the metadata for a tool definition.
type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ClientConfig holds configuration for connecting to an LLM service.
type ClientConfig struct {
	BaseURL string        // e.g., "https://api.openai.com/v1"
	APIKey  string        // Bearer token
	Model   string        // Default model override
	Timeout time.Duration // Request timeout
}

// Client sends requests to an OpenAI-compatible chat completion API.
type Client struct {
	cfg    ClientConfig
	client *http.Client
}

// NewClient creates a new LLM client from configuration.
func NewClient(cfg ClientConfig) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}
	return &Client{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// ChatRequest represents the payload for a chat completion call.
type ChatRequest struct {
	Model         string       `json:"model"`
	Messages      []Message    `json:"messages"`
	Tools         []ToolDef    `json:"tools,omitempty"`
	Stream        bool         `json:"stream,omitempty"`
	Temperature   *float64     `json:"temperature,omitempty"`
}

// Completions sends a chat completion request and returns the parsed response.
func (c *Client) Completions(req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = c.cfg.Model
	}

	payload, _ := json.Marshal(req)
	httpReq, err := http.NewRequest(http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResp struct {
		ID      string   `json:"id"`
		Model   string   `json:"model"`
		Choices []Choice `json:"choices"`
		Usage   *Usage   `json:"usage,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &ChatResponse{
		ID:      apiResp.ID,
		Model:   apiResp.Model,
		Choices: apiResp.Choices,
		Usage:   apiResp.Usage,
		Headers: resp.Header,
	}, nil
}

// GeneralRequest sends a simple chat request without or with optional tool calls (for plan phase, compression, etc.).
func (c *Client) GeneralRequest(messages []Message, model string, tools []ToolDef) (*ChatResponse, error) {
	return c.Completions(ChatRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
	})
}

// CountTokens is a stub — callers should integrate tiktoken or an external tokenizer service.
// Returns an estimate based on character count (~4 chars per token for English).
func CountTokens(text string) int {
	return len([]byte(text)) / 4
}

// StreamCompletion initiates a streaming chat completion. The callback is invoked per chunk.
func (c *Client) StreamCompletion(req ChatRequest, cb func(chunk []byte) error) error {
	req.Stream = true

	model := req.Model
	if model == "" {
		model = c.cfg.Model
	}

	body := make(map[string]any)
	b, _ := json.Marshal(req)
	json.Unmarshal(b, &body)
	body["model"] = model

	payload, _ := json.Marshal(body)
	httpReq, err := http.NewRequest(http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		if err := cb([]byte(data)); err != nil {
			return err
		}
	}
	return scanner.Err()
}
