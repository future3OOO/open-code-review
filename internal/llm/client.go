// Package llm provides an OpenAI-compatible LLM client interface.
// OpenCodeReview supports any service that implements the OpenAI Chat Completion API schema,
// including OpenAI, Claude (via Anthropic's OpenAI-compatible endpoint), local models, etc.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

const maxRetries = 10 // Maximum number of retry attempts with exponential backoff.

// Message represents a single message in a chat conversation.
// Content can be either plain string (for system/user/assistant/tool messages)
// or an array of content blocks (used by Claude for multi-part content).
// ToolCallID is used by OpenAI-format APIs to identify which tool call this result responds to.
type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`                // string or []ContentBlock
	ToolCallID string     `json:"tool_call_id,omitempty"` // OpenAI tool result identifier
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // assistant tool invocations
}

// ContentBlock represents a single block within a multi-part message content.
// Used by Claude's Messages API for tool results and multimodal content.
type ContentBlock struct {
	Type     string          `json:"type"`                       // "text" or "tool_result"
	Text     string          `json:"text,omitempty"`             // for type="text"
	ToolUseID string         `json:"tool_use_id,omitempty"`      // for type="tool_result"
	Content  []ContentBlock  `json:"content,omitempty"`          // nested text blocks inside tool_result
}

// NewTextMessage creates a message with simple string content.
func NewTextMessage(role, content string) Message {
	return Message{Role: role, Content: content}
}

// NewToolCallMessage creates an assistant message with text content and tool invocations.
func NewToolCallMessage(content string, toolCalls []ToolCall) Message {
	var tc []ToolCall
	if len(toolCalls) > 0 {
		tc = make([]ToolCall, len(toolCalls))
		copy(tc, toolCalls)
	}
	return Message{Role: "assistant", Content: content, ToolCalls: tc}
}

// NewToolResultMessage creates a tool-role message with the given result.
// Uses the OpenAI Chat Completions format: role="tool" with tool_call_id and plain string content.
func NewToolResultMessage(toolCallID, result string) Message {
	return Message{
		Role:       "tool",
		Content:    result,
		ToolCallID: toolCallID,
	}
}

// ExtractText returns the concatenated text content from a Message's Content field.
// Handles both plain string and content block array formats.
func (m *Message) ExtractText() string {
	switch v := m.Content.(type) {
	case string:
		return v
	case []ContentBlock:
		var sb strings.Builder
		for _, block := range v {
			sb.WriteString(extractBlockText(block))
		}
		return sb.String()
	default:
		return ""
	}
}

func extractBlockText(block ContentBlock) string {
	if block.Text != "" {
		return block.Text
	}
	var sb strings.Builder
	for _, nested := range block.Content {
		sb.WriteString(extractBlockText(nested))
	}
	return sb.String()
}


// Choice holds a single choice from the response.
type Choice struct {
	Message      ResponseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// ToolCall represents a function call requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
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
	ID      string      `json:"-"`
	Model   string      `json:"-"`
	Choices []Choice    `json:"-"`
	Headers http.Header `json:"-"` // Raw response headers (may contain session IDs, etc.)
	Usage   *UsageInfo  `json:"-"` // Token usage extracted from API response
}

// Content extracts the text content from the first choice, falling back to reasoning content.
func (r *ChatResponse) Content() string {
	if len(r.Choices) == 0 {
		return ""
	}
	msg := r.Choices[0].Message
	if msg.Content != nil && *msg.Content != "" {
		cleaned := stripThinkTags(*msg.Content)
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
	URL string // Full API endpoint URL (e.g., "https://api.openai.com/v1/chat/completions")
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
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
}

// Completions sends a chat completion request and returns the parsed response.
func (c *Client) Completions(req ChatRequest) (*ChatResponse, error) {
	return c.CompletionsWithCtx(context.Background(), req)
}

// CompletionsWithCtx sends a chat completion request with context support for cancellation and timeout.
func (c *Client) CompletionsWithCtx(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = c.cfg.Model
	}

	var result *ChatResponse
	err := c.withRetryCtx(ctx, func() error {
		resp, err := c.doRequestCtx(ctx, model, req)
		if err != nil {
			return err
		}
		result = resp
		return nil
	})
	return result, err
}

// GeneralRequest sends a simple chat request without or with optional tool calls (for plan phase, compression, etc.).
func (c *Client) GeneralRequest(messages []Message, model string, tools []ToolDef) (*ChatResponse, error) {
	return c.GeneralRequestWithCtx(context.Background(), messages, model, tools)
}

// GeneralRequestWithCtx sends a simple chat request with context support.
func (c *Client) GeneralRequestWithCtx(ctx context.Context, messages []Message, model string, tools []ToolDef) (*ChatResponse, error) {
	return c.CompletionsWithCtx(ctx, ChatRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
	})
}

// --- Token counting with tiktoken ---

var (
	tokenizer     *tiktoken.Tiktoken
	tokenizerOnce sync.Once
	tokenizerMu   sync.RWMutex
)

// SetModelEncoding selects the tiktoken encoding best suited for the given model name.
// It is safe to call multiple times; subsequent calls replace the previous tokenizer.
func SetModelEncoding(modelName string) error {
	var encName string
	lower := strings.ToLower(modelName)

	switch {
	// OpenAI latest models
	case strings.Contains(lower, "o1") || strings.Contains(lower, "o3") || strings.Contains(lower, "o4"):
		encName = "o200k_base"
	case strings.Contains(lower, "gpt-4o") || strings.Contains(lower, "gpt-4-turbo"):
		encName = "cl100k_base"
	case strings.Contains(lower, "gpt-4"):
		encName = "cl100k_base"
	case strings.Contains(lower, "gpt-3.5"):
		encName = "cl100k_base"
	// Claude (Anthropic uses cl100k-base-derived encoder; this is the closest public approximation)
	case strings.Contains(lower, "claude"):
		encName = "cl100k_base"
	default:
		encName = "cl100k_base"
	}

	enc, err := tiktoken.GetEncoding(encName)
	if err != nil {
		return fmt.Errorf("get tiktoken encoding %q: %w", encName, err)
	}

	tokenizerMu.Lock()
	tokenizer = enc
	tokenizerMu.Unlock()
	return nil
}

// ensureTokenizer lazily initializes the default tokenizer once.
func ensureTokenizer() {
	tokenizerOnce.Do(func() {
		if err := SetModelEncoding(""); err != nil {
			// Network unavailable or encoding load failed — fall back to byte estimation in CountTokens.
			fmt.Fprintf(os.Stderr, "[ocr] WARNING: tiktoken initialization failed (%v), using byte-based estimation\n", err)
		}
	})
}

// CountTokens returns the number of tokens in text using tiktoken BPE encoding.
// Before any explicit SetModelEncoding call, defaults to cl100k_base.
func CountTokens(text string) int {
	if text == "" {
		return 0
	}
	ensureTokenizer()

	tokenizerMu.RLock()
	tke := tokenizer
	tokenizerMu.RUnlock()

	if tke == nil {
		return len([]byte(text)) / 4
	}
	return len(tke.Encode(text, nil, nil))
}

// StreamCompletion initiates a streaming chat completion. The callback is invoked per chunk.
func (c *Client) StreamCompletion(req ChatRequest, cb func(chunk []byte) error) error {
	req.Stream = true

	model := req.Model
	if model == "" {
		model = c.cfg.Model
	}

	return c.withRetry(func() error {
		body := make(map[string]any)
		b, _ := json.Marshal(req)
		json.Unmarshal(b, &body)
		body["model"] = model

		payload, _ := json.Marshal(body)
		httpReq, err := http.NewRequest(http.MethodPost, c.cfg.URL, bytes.NewReader(payload))
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

		if isRetryableStatus(resp.StatusCode) {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("API error %d: %s", resp.StatusCode, string(bodyBytes))
		}
		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("API error %d: %s (non-retryable)", resp.StatusCode, string(bodyBytes))
		}

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
	})
}

// --- Retry logic ---

// stripThinkTags removes reasoning wrapper tags from content.
func stripThinkTags(s string) string {
	// Construct tag strings from individual bytes.
	openBytes := []byte{0x3c, 't', 'h', 'i', 'n', 'k', 0x3e}
	closeBytes := []byte{0x3c, 0x2f, 't', 'h', 'i', 'n', 'k', 0x3e}
	s = strings.ReplaceAll(s, string(openBytes), "")
	s = strings.ReplaceAll(s, string(closeBytes), "")
	return s
}

func (c *Client) withRetry(fn func() error) error {
	return c.withRetryCtx(context.Background(), fn)
}

func (c *Client) withRetryCtx(ctx context.Context, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled: %w", ctx.Err())
		default:
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if !isRetryable(lastErr) {
			return lastErr
		}

		if attempt < maxRetries {
			sleepWithBackoff(attempt)
		}
	}
	return fmt.Errorf("request failed after %d retries: %w", maxRetries, lastErr)
}

// isRetryable determines whether an error is transient and worth retrying.
func isRetryable(err error) bool {
	msg := err.Error()
	// 429 (rate limit) and 5xx server errors are retryable.
	if strings.Contains(msg, "API error 429:") {
		return true
	}
	for code := 500; code <= 599; code++ {
		if strings.Contains(msg, fmt.Sprintf("API error %d:", code)) {
			return true
		}
	}
	// Network-level errors (timeout, connection refused, DNS failure, etc.) are retryable.
	if strings.Contains(msg, "request failed:") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "EOF") {
		return true
	}
	return false
}

// isRetryableStatus returns true for HTTP status codes that should trigger a retry.
func isRetryableStatus(status int) bool {
	return status == 429 || (status >= 500 && status <= 599)
}

// sleepWithBackoff sleeps for baseDelay * 2^attempt + jitter, capped at 60s.
// Jitter spreads retries randomly within ±50% of the computed delay.
func sleepWithBackoff(attempt int) {
	const (
		baseDelay = 1 * time.Second
		maxDelay  = 60 * time.Second
	)

	delay := baseDelay << uint(min(attempt, 6)) // 1s, 2s, 4s, 8s, 16s, 32s, 64s→capped
	if delay > maxDelay {
		delay = maxDelay
	}

	// Add random jitter: [delay*0.5, delay*1.5]
	jitter := time.Duration(rand.Int63n(int64(delay))) - delay/2
	delay += jitter

	fmt.Printf("[llm] Retrying in %v (attempt info)... \n", delay)
	time.Sleep(delay)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// doRequest builds and sends a non-streaming completion request, returning the parsed response.
func (c *Client) doRequest(model string, req ChatRequest) (*ChatResponse, error) {
	return c.doRequestCtx(context.Background(), model, req)
}

// doRequestCtx builds and sends a non-streaming completion request with context support.
func (c *Client) doRequestCtx(ctx context.Context, model string, req ChatRequest) (*ChatResponse, error) {
	if model == "" {
		model = c.cfg.Model
	}
	req.Model = model
	payload, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(payload))
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResp struct {
		ID      string   `json:"id"`
		Model   string   `json:"model"`
		Choices []Choice `json:"choices"`
	}
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &ChatResponse{
		ID:      apiResp.ID,
		Model:   apiResp.Model,
		Choices: apiResp.Choices,
		Headers: resp.Header,
		Usage:   resolveUsage(bodyBytes),
	}, nil
}
