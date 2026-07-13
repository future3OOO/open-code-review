package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const defaultCodexCodeTimeout = 5 * time.Minute
const codexCodeProtocolSchema = `{"type":"object","properties":{"content":{"type":"string"},"tool_calls":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"arguments":{"type":"string"}},"required":["id","name","arguments"],"additionalProperties":false}}},"required":["content","tool_calls"],"additionalProperties":false}`

var codexCodeEnvNames = []string{
	"HOME",
	"PATH",
	"SHELL",
	"USER",
	"LOGNAME",
	"TMPDIR",
	"LANG",
	"LC_ALL",
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"NO_PROXY",
	"http_proxy",
	"https_proxy",
	"no_proxy",
	"SSL_CERT_FILE",
	"SSL_CERT_DIR",
	"CODEX_HOME",
}

var runCodexCodeCommand = func(ctx context.Context, command []string, prompt string, env []string) ([]byte, error) {
	return runCodeAgentCommand(ctx, "codex-code", command, prompt, env, nil)
}

type CodexCodeClient struct {
	cfg ClientConfig
}

type codexCodeEvent struct {
	Type string `json:"type"`
	Item *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
	Usage *struct {
		InputTokens       *int64 `json:"input_tokens"`
		CachedInputTokens int64  `json:"cached_input_tokens"`
		OutputTokens      *int64 `json:"output_tokens"`
	} `json:"usage"`
}

func NewCodexCodeClient(cfg ClientConfig) *CodexCodeClient {
	return &CodexCodeClient{cfg: cfg}
}

func (c *CodexCodeClient) CompletionsWithCtx(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = c.cfg.Model
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("codex model is required")
	}

	schema, err := os.CreateTemp("", "open-code-review-codex-schema-*.json")
	if err != nil {
		return nil, fmt.Errorf("create codex output schema: %w", err)
	}
	schemaPath := schema.Name()
	defer os.Remove(schemaPath)
	if _, err := schema.WriteString(codexCodeProtocolSchema); err != nil {
		schema.Close()
		return nil, fmt.Errorf("write codex output schema: %w", err)
	}
	if err := schema.Close(); err != nil {
		return nil, fmt.Errorf("close codex output schema: %w", err)
	}

	command := []string{
		"codex", "exec", "--ephemeral", "--sandbox", "read-only",
		"--skip-git-repo-check", "--model", model,
		"-c", `model_reasoning_effort="medium"`,
		"-c", "features.shell_tool=false",
		"-c", "features.unified_exec=false",
		"-c", "features.multi_agent=false",
		"-c", "features.apps=false",
		"-c", "features.plugins=false",
		"-c", "mcp_servers={}",
		"-c", `web_search="disabled"`,
		"--color", "never", "--json", "--output-schema", schemaPath, "-",
	}
	prompt, err := renderCodeAgentPrompt("Codex", codexCodeProtocolSchema, req)
	if err != nil {
		return nil, err
	}
	if _, ok := ctx.Deadline(); !ok {
		timeout := c.cfg.Timeout
		if timeout <= 0 {
			timeout = defaultCodexCodeTimeout
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	out, err := runCodexCodeCommand(ctx, command, prompt, codexCodeEnvironment())
	if err != nil {
		return nil, err
	}
	return parseCodexCodeResponse(out, model, req.Tools)
}

func parseCodexCodeResponse(raw []byte, model string, allowedTools []ToolDef) (*ChatResponse, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var finalOutput string
	var usage *UsageInfo
	for {
		var event codexCodeEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("parse codex event stream: %w", err)
		}
		if event.Type == "item.completed" && event.Item != nil && event.Item.Type == "agent_message" {
			finalOutput = event.Item.Text
		}
		if event.Type == "turn.completed" {
			if event.Usage == nil || event.Usage.InputTokens == nil || event.Usage.OutputTokens == nil ||
				*event.Usage.InputTokens < 0 || event.Usage.CachedInputTokens < 0 || *event.Usage.OutputTokens < 0 {
				return nil, fmt.Errorf("codex turn completed without valid usage")
			}
			usage = &UsageInfo{
				PromptTokens:     *event.Usage.InputTokens,
				CompletionTokens: *event.Usage.OutputTokens,
				TotalTokens:      *event.Usage.InputTokens + *event.Usage.OutputTokens,
				CacheReadTokens:  event.Usage.CachedInputTokens,
			}
		}
	}
	if finalOutput == "" {
		return nil, fmt.Errorf("codex event stream did not contain a completed agent message")
	}
	if usage == nil {
		return nil, fmt.Errorf("codex event stream did not contain terminal usage")
	}
	response, err := parseClaudeCodeResponse([]byte(finalOutput), model, allowedTools)
	if err != nil {
		return nil, err
	}
	response.Usage = usage
	return response, nil
}

func codexCodeEnvironment() []string {
	return codeAgentEnvironment(codexCodeEnvNames)
}
