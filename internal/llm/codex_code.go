package llm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

const defaultCodexCodeTimeout = 5 * time.Minute

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
	if _, err := schema.WriteString(claudeCodeProtocolSchema); err != nil {
		schema.Close()
		return nil, fmt.Errorf("write codex output schema: %w", err)
	}
	if err := schema.Close(); err != nil {
		return nil, fmt.Errorf("close codex output schema: %w", err)
	}

	command := []string{
		"codex", "exec", "--ephemeral", "--sandbox", "read-only",
		"--skip-git-repo-check", "--model", model,
		"-c", "features.shell_tool=false",
		"-c", "features.unified_exec=false",
		"-c", "features.multi_agent=false",
		"-c", "features.apps=false",
		"-c", "features.plugins=false",
		"-c", "mcp_servers={}",
		"-c", `web_search="disabled"`,
		"--color", "never", "--output-schema", schemaPath, "-",
	}
	prompt, err := renderCodeAgentPrompt("Codex", req)
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
	return parseClaudeCodeResponse(out, model, req.Tools)
}

func codexCodeEnvironment() []string {
	return codeAgentEnvironment(codexCodeEnvNames)
}
