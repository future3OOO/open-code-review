package llm

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestCodexCodeClientReturnsToolCallsFromStructuredOutput(t *testing.T) {
	oldRunner := runCodexCodeCommand
	var schemaPath string
	runCodexCodeCommand = func(_ context.Context, command []string, prompt string, _ []string) ([]byte, error) {
		for _, required := range []string{
			"exec", "--ephemeral", "--sandbox", "read-only", "--skip-git-repo-check",
			"--model", "gpt-5.4", "features.shell_tool=false",
			"features.unified_exec=false", "features.multi_agent=false",
			"features.apps=false", "features.plugins=false", `web_search="disabled"`,
			"mcp_servers={}", "--color", "never", "--output-schema", "-",
		} {
			if !slices.Contains(command, required) {
				t.Fatalf("command missing %q: %#v", required, command)
			}
		}
		schemaPath = command[slices.Index(command, "--output-schema")+1]
		schema, err := os.ReadFile(schemaPath)
		if err != nil {
			t.Fatalf("read output schema: %v", err)
		}
		if !json.Valid(schema) || !strings.Contains(prompt, "trusted_tools") {
			t.Fatalf("schema = %s; prompt = %s", schema, prompt)
		}
		return []byte(`{"content":"","tool_calls":[{"id":"call-1","name":"code_comment","arguments":{"comments":[]}}]}`), nil
	}
	t.Cleanup(func() { runCodexCodeCommand = oldRunner })

	client := NewLLMClient(ResolvedEndpoint{Protocol: "codex-code", Model: "gpt-5.4"})
	response, err := client.CompletionsWithCtx(context.Background(), ChatRequest{
		Messages: []Message{NewTextMessage("user", "review")},
		Tools: []ToolDef{{
			Type: "function",
			Function: FunctionDef{
				Name:       "code_comment",
				Parameters: map[string]any{"type": "object"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("completion failed: %v", err)
	}
	calls := response.ToolCalls()
	if len(calls) != 1 || calls[0].Function.Name != "code_comment" || calls[0].Function.Arguments != `{"comments":[]}` {
		t.Fatalf("tool calls = %#v", calls)
	}
	if _, err := os.Stat(schemaPath); !os.IsNotExist(err) {
		t.Fatalf("schema file was not removed: %v", err)
	}
}

func TestCodexCodeEnvironmentForwardsOnlyRuntimeConfiguration(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("CODEX_HOME", "/tmp/codex-home")
	t.Setenv("UNRELATED_SECRET", "must-not-leak")

	env := codexCodeEnvironment()
	if !containsAll(env, "PATH=/usr/bin", "CODEX_HOME=/tmp/codex-home") {
		t.Fatalf("missing required environment: %v", env)
	}
	if slices.Contains(env, "UNRELATED_SECRET=must-not-leak") {
		t.Fatalf("unrelated secret leaked into Codex environment: %v", env)
	}
}
