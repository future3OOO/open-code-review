package llm

import (
	"context"
	"encoding/json"
	"fmt"
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
			"--model", "gpt-5.6-sol", "--color", "never", "--json", "--output-schema", "-",
		} {
			if !slices.Contains(command, required) {
				t.Fatalf("command missing %q: %#v", required, command)
			}
		}
		for _, override := range []string{
			`model_reasoning_effort="medium"`, "features.shell_tool=false",
			"features.unified_exec=false", "features.multi_agent=false",
			"features.apps=false", "features.plugins=false", "mcp_servers={}",
			`web_search="disabled"`,
		} {
			index := slices.Index(command, override)
			if index < 1 || command[index-1] != "-c" {
				t.Fatalf("command missing adjacent -c %q: %#v", override, command)
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
		if !strings.Contains(string(schema), `"arguments":{"type":"string","description":"JSON-encoded object matching the selected tool's parameters"}`) ||
			!strings.Contains(prompt, `"arguments":{"type":"string","description":"JSON-encoded object matching the selected tool's parameters"}`) {
			t.Fatalf("Codex arguments contract is not a JSON string: schema = %s; prompt = %s", schema, prompt)
		}
		return []byte("{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"{\\\"content\\\":\\\"\\\",\\\"tool_calls\\\":[{\\\"id\\\":\\\"call-1\\\",\\\"name\\\":\\\"code_comment\\\",\\\"arguments\\\":\\\"{\\\\\\\"comments\\\\\\\":[]}\\\"}]}\"}}\n" +
			"{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":24763,\"cached_input_tokens\":24448,\"output_tokens\":122,\"reasoning_output_tokens\":0}}\n"), nil
	}
	t.Cleanup(func() { runCodexCodeCommand = oldRunner })

	client := NewLLMClient(ResolvedEndpoint{Protocol: "codex-code", Model: "gpt-5.6-sol"})
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
	if response.Usage == nil || response.Usage.PromptTokens != 24763 || response.Usage.CompletionTokens != 122 || response.Usage.TotalTokens != 24885 || response.Usage.CacheReadTokens != 24448 {
		t.Fatalf("usage = %#v", response.Usage)
	}
	if _, err := os.Stat(schemaPath); !os.IsNotExist(err) {
		t.Fatalf("schema file was not removed: %v", err)
	}
}

func TestCodexCodeClientLabelsPromptMarshalFailure(t *testing.T) {
	client := NewLLMClient(ResolvedEndpoint{Protocol: "codex-code", Model: "gpt-5.6-sol"})
	_, err := client.CompletionsWithCtx(context.Background(), ChatRequest{
		Tools: []ToolDef{{Function: FunctionDef{
			Name:       "invalid",
			Parameters: map[string]any{"invalid": make(chan int)},
		}}},
	})
	if err == nil || !strings.Contains(err.Error(), "marshal Codex prompt") {
		t.Fatalf("error = %v, want Codex prompt marshal context", err)
	}
}

func TestCodexCodeClientRejectsMissingTerminalUsage(t *testing.T) {
	oldRunner := runCodexCodeCommand
	runCodexCodeCommand = func(_ context.Context, _ []string, _ string, _ []string) ([]byte, error) {
		return []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"{\"content\":\"ok\",\"tool_calls\":[]}"}}`), nil
	}
	t.Cleanup(func() { runCodexCodeCommand = oldRunner })

	client := NewLLMClient(ResolvedEndpoint{Protocol: "codex-code", Model: "gpt-5.6-sol"})
	_, err := client.CompletionsWithCtx(context.Background(), ChatRequest{Messages: []Message{NewTextMessage("user", "review")}})
	if err == nil || !strings.Contains(err.Error(), "terminal usage") {
		t.Fatalf("error = %v, want missing terminal usage", err)
	}
}

func TestCodexCodeClientRetriesOnlyMalformedArgumentString(t *testing.T) {
	oldRunner := runCodexCodeCommand
	malformed := `{"content":"","tool_calls":[{"id":"call-1","name":"code_comment","arguments":"not json"}]}`
	responses := []string{
		malformed,
		`{"content":"","tool_calls":[{"id":"call-1","name":"code_comment","arguments":"{\"comments\":[]}"}]}`,
	}
	calls := 0
	runCodexCodeCommand = func(_ context.Context, _ []string, _ string, _ []string) ([]byte, error) {
		response := responses[calls]
		calls++
		return []byte(fmt.Sprintf(
			"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":%q}}\n"+
				"{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":100,\"cached_input_tokens\":80,\"output_tokens\":20}}\n",
			response,
		)), nil
	}
	t.Cleanup(func() { runCodexCodeCommand = oldRunner })

	client := NewLLMClient(ResolvedEndpoint{Protocol: "codex-code", Model: "gpt-5.6-sol"})
	request := ChatRequest{
		Messages: []Message{NewTextMessage("user", "review")},
		Tools: []ToolDef{{
			Type: "function",
			Function: FunctionDef{
				Name:       "code_comment",
				Parameters: map[string]any{"type": "object"},
			},
		}},
	}
	response, err := client.CompletionsWithCtx(context.Background(), request)
	if err != nil {
		t.Fatalf("completion failed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("Codex calls = %d, want one corrective retry", calls)
	}
	toolCalls := response.ToolCalls()
	if len(toolCalls) != 1 || toolCalls[0].Function.Arguments != `{"comments":[]}` {
		t.Fatalf("tool calls = %#v", toolCalls)
	}
	if response.Usage == nil || response.Usage.PromptTokens != 200 || response.Usage.CompletionTokens != 40 || response.Usage.TotalTokens != 240 || response.Usage.CacheReadTokens != 160 {
		t.Fatalf("usage = %#v", response.Usage)
	}

	responses = []string{malformed, malformed}
	calls = 0
	if _, err := client.CompletionsWithCtx(context.Background(), request); err == nil || err.Error() != "claude-code tool call arguments string was not JSON" {
		t.Fatalf("second malformed completion error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("Codex calls after second malformed response = %d, want 2", calls)
	}

	responses = []string{`{"content":"","tool_calls":[{"id":"call-1","name":"unavailable","arguments":"{}"}]}`}
	calls = 0
	if _, err := client.CompletionsWithCtx(context.Background(), request); err == nil || !strings.Contains(err.Error(), "unavailable tool") {
		t.Fatalf("unavailable tool error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("Codex calls after unrelated error = %d, want 1", calls)
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
