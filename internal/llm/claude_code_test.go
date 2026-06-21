package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestClaudeCodeClientReturnsToolCallsFromStructuredOutput(t *testing.T) {
	var gotCommand []string
	var gotPrompt string
	var gotEnv []string
	withClaudeCodeRunner(t, func(ctx context.Context, command []string, prompt string, env []string) ([]byte, error) {
		gotCommand = append([]string(nil), command...)
		gotPrompt = prompt
		gotEnv = append([]string(nil), env...)
		return []byte(`{
			"type": "result",
			"subtype": "success",
			"is_error": false,
			"permission_denials": [],
			"structured_output": {
				"content": "",
				"tool_calls": [
					{
						"id": "call_1",
						"name": "code_comment",
						"arguments": {
							"path": "app.go",
							"content": "Check the nil path.",
							"start_line": 12,
							"end_line": 12
						}
					}
				]
			}
		}`), nil
	})

	t.Setenv("HOME", "/tmp/claude-home")
	t.Setenv("PATH", "/bin")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "claude-code-oauth")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-api-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "anthropic-auth-token")
	t.Setenv("GITHUB_TOKEN", "github-token")

	resp, err := runClaudeCodeCompletion(t, ChatRequest{
		Messages: []Message{NewTextMessage("user", "Review app.go.")},
		Tools: []ToolDef{
			{
				Type: "function",
				Function: FunctionDef{
					Name:        "code_comment",
					Description: "Add an advisory code review comment.",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"path": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
		MaxTokens: 4096,
	})
	if err != nil {
		t.Fatalf("CompletionsWithCtx: %v", err)
	}

	if !containsAll(gotCommand, "claude", "--print", "--model", "claude-opus-4-8", "--allowedTools", "") {
		t.Fatalf("unexpected command: %v", gotCommand)
	}
	if !strings.Contains(gotPrompt, "Review app.go.") || !strings.Contains(gotPrompt, "code_comment") {
		t.Fatalf("prompt did not include OCR messages and tools: %s", gotPrompt)
	}
	envText := strings.Join(gotEnv, "\n")
	if !strings.Contains(envText, "CLAUDE_CODE_OAUTH_TOKEN=claude-code-oauth") {
		t.Fatalf("expected Claude Code token in env: %v", gotEnv)
	}
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "GITHUB_TOKEN"} {
		if strings.Contains(envText, name+"=") {
			t.Fatalf("unexpected secret %s in env: %v", name, gotEnv)
		}
	}

	calls := resp.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].ID != "call_1" || calls[0].Function.Name != "code_comment" {
		t.Fatalf("unexpected tool call: %+v", calls[0])
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("tool call arguments are not JSON: %v", err)
	}
	if args["path"] != "app.go" || args["content"] != "Check the nil path." {
		t.Fatalf("unexpected arguments: %v", args)
	}
}

func TestClaudeCodeClientFailsOnMalformedOutput(t *testing.T) {
	withClaudeCodeRunner(t, func(ctx context.Context, command []string, prompt string, env []string) ([]byte, error) {
		return []byte(`{"structured_output":{"tool_calls":[{"name":""}]}}`), nil
	})

	_, err := runClaudeCodeCompletion(t, simpleClaudeCodeReviewRequest())
	if err == nil {
		t.Fatal("expected malformed output error")
	}
}

func TestClaudeCodeClientReturnsRunnerErrors(t *testing.T) {
	withClaudeCodeRunner(t, func(ctx context.Context, command []string, prompt string, env []string) ([]byte, error) {
		return nil, errors.New("claude failed")
	})

	_, err := runClaudeCodeCompletion(t, simpleClaudeCodeReviewRequest())
	if err == nil || !strings.Contains(err.Error(), "claude failed") {
		t.Fatalf("expected runner error, got %v", err)
	}
}

func TestClaudeCodeClientFailsOnPromptMarshalError(t *testing.T) {
	withClaudeCodeRunner(t, func(ctx context.Context, command []string, prompt string, env []string) ([]byte, error) {
		t.Fatal("runner should not be called when prompt rendering fails")
		return nil, nil
	})

	client := NewLLMClient(ResolvedEndpoint{Protocol: "claude-code", Model: "claude-opus-4-8"})
	_, err := client.CompletionsWithCtx(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: func() {}}},
	})
	if err == nil || !strings.Contains(err.Error(), "marshal claude-code messages") {
		t.Fatalf("expected prompt marshal error, got %v", err)
	}
}

func TestParseClaudeCodeResponseAcceptsRealStructuredOutputEnvelope(t *testing.T) {
	resp, err := parseClaudeCodeResponse([]byte(`{
		"type": "result",
		"subtype": "success",
		"is_error": false,
		"result": "smoke-ok",
		"permission_denials": [],
		"structured_output": {
			"content": "smoke-ok",
			"tool_calls": []
		},
		"terminal_reason": "completed"
	}`), "claude-opus-4-8")
	if err != nil {
		t.Fatalf("parseClaudeCodeResponse: %v", err)
	}
	if resp.Content() != "smoke-ok" {
		t.Fatalf("Content = %q, want smoke-ok", resp.Content())
	}
}

func TestNormalizeClaudeCodeArgumentsRequiresJSONObject(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`[]`),
		json.RawMessage(`42`),
		json.RawMessage(`"[1,2]"`),
	} {
		if _, err := normalizeClaudeCodeArguments(raw); err == nil {
			t.Fatalf("expected object validation error for %s", raw)
		}
	}
}

func TestRunClaudeCodeCommandIgnoresSuccessfulStderr(t *testing.T) {
	out, err := runClaudeCodeCommand(
		context.Background(),
		[]string{"sh", "-c", "printf '{\"ok\":true}'; printf warning >&2"},
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("runClaudeCodeCommand: %v", err)
	}
	if string(out) != `{"ok":true}` {
		t.Fatalf("stdout = %q, want JSON without stderr", out)
	}
}

func withClaudeCodeRunner(t *testing.T, runner func(context.Context, []string, string, []string) ([]byte, error)) {
	t.Helper()

	oldRunner := runClaudeCodeCommand
	runClaudeCodeCommand = runner
	t.Cleanup(func() { runClaudeCodeCommand = oldRunner })
}

func runClaudeCodeCompletion(t *testing.T, req ChatRequest) (*ChatResponse, error) {
	t.Helper()

	client := NewLLMClient(ResolvedEndpoint{Protocol: "claude-code", Model: "claude-opus-4-8"})
	return client.CompletionsWithCtx(context.Background(), req)
}

func simpleClaudeCodeReviewRequest() ChatRequest {
	return ChatRequest{
		Messages: []Message{NewTextMessage("user", "Review.")},
	}
}

func containsAll(values []string, required ...string) bool {
	for _, need := range required {
		found := false
		for _, value := range values {
			if value == need {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
