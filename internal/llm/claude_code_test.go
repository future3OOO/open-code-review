package llm

import (
	"context"
	"encoding/json"
	"os"
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
		return []byte(`{"type":"result","subtype":"success","is_error":false,"permission_denials":[],"structured_output":{"content":"","tool_calls":[{"id":"call_1","name":"code_comment","arguments":{"path":"app.go","content":"Check the nil path.","start_line":12,"end_line":12}}]}}`), nil
	})

	t.Setenv("HOME", "/tmp/claude-home")
	t.Setenv("PATH", "/bin")
	t.Setenv("HTTPS_PROXY", "http://proxy.internal:8080")
	t.Setenv("NODE_EXTRA_CA_CERTS", "/etc/ssl/custom-ca.pem")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "claude-code-oauth")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-api-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "anthropic-auth-token")
	t.Setenv("GITHUB_TOKEN", "github-token")

	resp := mustRunClaudeCodeCompletion(t, ChatRequest{
		Messages: []Message{NewTextMessage("user", "Review app.go.")},
		Tools:    []ToolDef{{Function: FunctionDef{Name: "code_comment", Parameters: map[string]any{"type": "object"}}}},
	})

	if !containsAll(gotCommand, "claude", "--print", "--model", "claude-opus-4-8", "--permission-mode", "default", "--tools", "--allowedTools", "--disallowedTools") {
		t.Fatalf("unexpected command: %v", gotCommand)
	}
	if flagValue(gotCommand, "--tools") != "" || flagValue(gotCommand, "--allowedTools") != "" || flagValue(gotCommand, "--disallowedTools") != "mcp__*" {
		t.Fatalf("unexpected tool flag values: %v", gotCommand)
	}
	if !strings.Contains(gotPrompt, `"untrusted_messages"`) || !strings.Contains(gotPrompt, "Review app.go.") || !strings.Contains(gotPrompt, "code_comment") {
		t.Fatalf("prompt did not include OCR messages and tools: %s", gotPrompt)
	}
	envText := strings.Join(gotEnv, "\n")
	if !strings.Contains(envText, "CLAUDE_CODE_OAUTH_TOKEN=claude-code-oauth") {
		t.Fatalf("expected Claude Code token in env: %v", gotEnv)
	}
	if !strings.Contains(envText, "HTTPS_PROXY=http://proxy.internal:8080") || !strings.Contains(envText, "NODE_EXTRA_CA_CERTS=/etc/ssl/custom-ca.pem") {
		t.Fatalf("expected network boundary env in Claude Code env: %v", gotEnv)
	}
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "GITHUB_TOKEN"} {
		if strings.Contains(envText, name+"=") {
			t.Fatalf("unexpected secret %s in env: %v", name, gotEnv)
		}
	}

	call := requireSingleClaudeCodeToolCall(t, resp)
	if resp.Choices[0].Message.Content != nil {
		t.Fatalf("expected nil content for tool-call-only response, got %q", *resp.Choices[0].Message.Content)
	}
	if call.ID != "call_1" || call.Function.Name != "code_comment" {
		t.Fatalf("unexpected tool call: %+v", call)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		t.Fatalf("tool call arguments are not JSON: %v", err)
	}
	if args["path"] != "app.go" || args["content"] != "Check the nil path." {
		t.Fatalf("unexpected arguments: %v", args)
	}
}

func TestClaudeCodeClientFailsOnMalformedOutput(t *testing.T) {
	withClaudeCodeRunner(t, func(ctx context.Context, command []string, prompt string, env []string) ([]byte, error) {
		return []byte(`{"structured_output":{"tool_calls":[{"id":"call_bad","name":""}]}}`), nil
	})

	_, err := runClaudeCodeCompletion(t, simpleClaudeCodeReviewRequest())
	if err == nil {
		t.Fatal("expected malformed output error")
	}
	if !strings.Contains(err.Error(), "index 0") || !strings.Contains(err.Error(), "call_bad") {
		t.Fatalf("malformed output error lacked tool call detail: %v", err)
	}
}

func TestClaudeCodeClientDefaultsMissingToolArgumentsToObject(t *testing.T) {
	withClaudeCodeRunner(t, func(ctx context.Context, command []string, prompt string, env []string) ([]byte, error) {
		return []byte(`{"content":"","tool_calls":[{"id":"call_1","name":"no_arg_tool"}]}`), nil
	})

	resp := mustRunClaudeCodeCompletion(t, ChatRequest{
		Messages: []Message{NewTextMessage("user", "Review.")},
		Tools: []ToolDef{{
			Type: "function",
			Function: FunctionDef{
				Name:       "no_arg_tool",
				Parameters: map[string]any{"type": "object"},
			},
		}},
	})
	call := requireSingleClaudeCodeToolCall(t, resp)
	if call.Function.Arguments != "{}" {
		t.Fatalf("Arguments = %q, want {}", call.Function.Arguments)
	}
}

func TestClaudeCodeClientRejectsUnavailableToolCall(t *testing.T) {
	withClaudeCodeRunner(t, func(ctx context.Context, command []string, prompt string, env []string) ([]byte, error) {
		return []byte(`{"content":"","tool_calls":[{"id":"call_1","name":"unexpected","arguments":{}}]}`), nil
	})

	_, err := runClaudeCodeCompletion(t, simpleClaudeCodeReviewRequest())
	if err == nil || !strings.Contains(err.Error(), "unavailable tool") {
		t.Fatalf("expected unavailable tool error, got %v", err)
	}
}

func TestClaudeCodeClientRejectsInvalidToolCallShape(t *testing.T) {
	assertRejected := func(raw, want string) {
		_, err := parseClaudeCodeResponse([]byte(raw), "claude-opus-4-8", []ToolDef{{Function: FunctionDef{Name: "code_comment"}}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %s error, got %v", want, err)
		}
	}
	assertRejected(`{"content":"","tool_calls":[{"id":"call_1","name":"code_comment","arguments":{}},{"id":"call_1","name":"code_comment","arguments":{}}]}`, "duplicate tool call id")
	var calls []string
	for i := 0; i <= maxClaudeCodeToolCalls; i++ {
		calls = append(calls, `{"id":"call_`+string(rune('a'+i/26))+string(rune('a'+i%26))+`","name":"code_comment","arguments":{}}`)
	}
	assertRejected(`{"content":"","tool_calls":[`+strings.Join(calls, ",")+`]}`, "tool calls")
}

func TestClaudeCodeClientAppliesDefaultDeadline(t *testing.T) {
	withClaudeCodeRunner(t, func(ctx context.Context, command []string, prompt string, env []string) ([]byte, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("expected claude-code runner context to have a deadline")
		}
		return []byte(`{"content":"ok","tool_calls":[]}`), nil
	})

	resp := mustRunClaudeCodeCompletion(t, simpleClaudeCodeReviewRequest())
	if resp.Content() != "ok" {
		t.Fatalf("Content = %q, want ok", resp.Content())
	}
}

func TestParseClaudeCodeResponseRejectsErrorEnvelopeBeforeDirectOutput(t *testing.T) {
	_, err := parseClaudeCodeResponse([]byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"content":"looks successful","tool_calls":[]}`), "claude-opus-4-8", nil)
	if err == nil || !strings.Contains(err.Error(), "error envelope") {
		t.Fatalf("expected error envelope rejection, got %v", err)
	}
}

func TestParseClaudeCodeResponseRejectsEmptyOutput(t *testing.T) {
	for _, raw := range []string{
		`{"content":"","tool_calls":null}`,
		`{"type":"result","subtype":"success","is_error":false,"permission_denials":[],"structured_output":{"content":"","tool_calls":[]}}`,
	} {
		if _, err := parseClaudeCodeResponse([]byte(raw), "claude-opus-4-8", nil); err == nil || !strings.Contains(err.Error(), "content or tool calls") {
			t.Fatalf("expected empty output error, got %v", err)
		}
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

func TestRunClaudeCodeCommandRedactsFailureStderr(t *testing.T) {
	_, err := runClaudeCodeCommand(
		context.Background(),
		[]string{"sh", "-c", "printf 'auth failed for secret-token' >&2; exit 1"},
		"",
		[]string{"CLAUDE_CODE_OAUTH_TOKEN=secret-token"},
	)
	if err == nil {
		t.Fatal("expected command error")
	}
	if strings.Contains(err.Error(), "secret-token") || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("unexpected command error: %v", err)
	}
}

func TestSensitiveClaudeCodeEnvLocksCurrentForwardedClassifications(t *testing.T) {
	wantSensitive := map[string]bool{
		"HTTP_PROXY":              true,
		"HTTPS_PROXY":             true,
		"NO_PROXY":                true,
		"http_proxy":              true,
		"https_proxy":             true,
		"no_proxy":                true,
		"CLAUDE_CODE_OAUTH_TOKEN": true,
	}
	for _, name := range claudeCodeEnvNames {
		if got := sensitiveClaudeCodeEnv(name); got != wantSensitive[name] {
			t.Fatalf("sensitiveClaudeCodeEnv(%q) = %v, want %v", name, got, wantSensitive[name])
		}
	}
}

func TestClaudeCodeEnvironmentDefaultsPath(t *testing.T) {
	oldPath, hadPath := os.LookupEnv("PATH")
	if err := os.Unsetenv("PATH"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadPath {
			os.Setenv("PATH", oldPath)
		} else {
			os.Unsetenv("PATH")
		}
	})

	envText := strings.Join(claudeCodeEnvironment(), "\n")
	if !strings.Contains(envText, "PATH=/usr/local/bin:/usr/bin:/bin") {
		t.Fatalf("expected default PATH, got %s", envText)
	}

	if err := os.Setenv("PATH", ""); err != nil {
		t.Fatal(err)
	}
	envText = strings.Join(claudeCodeEnvironment(), "\n")
	if !strings.Contains(envText, "PATH=/usr/local/bin:/usr/bin:/bin") {
		t.Fatalf("expected default PATH for empty PATH, got %s", envText)
	}
}

func TestRunClaudeCodeCommandRejectsOversizedStdout(t *testing.T) {
	_, err := runClaudeCodeCommand(
		context.Background(),
		[]string{"sh", "-c", "head -c 4194305 /dev/zero"},
		"",
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "output exceeded") {
		t.Fatalf("expected output limit error, got %v", err)
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

func mustRunClaudeCodeCompletion(t *testing.T, req ChatRequest) *ChatResponse {
	t.Helper()

	resp, err := runClaudeCodeCompletion(t, req)
	if err != nil {
		t.Fatalf("CompletionsWithCtx: %v", err)
	}
	return resp
}

func requireSingleClaudeCodeToolCall(t *testing.T, resp *ChatResponse) ToolCall {
	t.Helper()

	calls := resp.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	return calls[0]
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

func flagValue(values []string, flag string) string {
	for i, value := range values {
		if value == flag && i+1 < len(values) {
			return values[i+1]
		}
	}
	return ""
}
