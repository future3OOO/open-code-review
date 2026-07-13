package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-code-review/open-code-review/internal/config/template"
	"github.com/open-code-review/open-code-review/internal/llm"
	"github.com/open-code-review/open-code-review/internal/model"
	"github.com/open-code-review/open-code-review/internal/tool"
)

func toolResponse(name string, arguments any) *llm.ChatResponse {
	return toolCallsResponse(toolCall(name, arguments))
}

func toolCall(name string, arguments any) llm.ToolCall {
	encoded, _ := json.Marshal(arguments)
	return llm.ToolCall{ID: name, Function: llm.FunctionCall{Name: name, Arguments: string(encoded)}}
}

func toolCallsResponse(calls ...llm.ToolCall) *llm.ChatResponse {
	return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.ResponseMessage{ToolCalls: calls}}}}
}

func textResponse(content string) *llm.ChatResponse {
	return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.ResponseMessage{Content: &content}}}}
}

func candidate(content, severity, failureMode, contract string) map[string]any {
	return map[string]any{
		"content": content, "severity": severity, "failure_mode": failureMode,
		"violated_contract": contract, "evidence": "allow is set without a check",
		"existing_code": "allow := true",
	}
}

func authCandidate() map[string]any {
	return candidate("authorization bypass", "high", "unauthorized requests are allowed", "requests must be authorized")
}

func authRevalidationFinding(existingCode string) model.LlmComment {
	return model.LlmComment{
		Path: "app.go", Content: "authorization bypass", Severity: "high",
		FailureMode: "unauthorized requests are allowed", ViolatedContract: "requests must be authorized",
		Evidence: "allow is set without a check", ExistingCode: existingCode, StartLine: 999, EndLine: 999,
	}
}

func reviewResponses(comments []map[string]any, verdict string) []*llm.ChatResponse {
	return append([]*llm.ChatResponse{toolResponse("code_comment", map[string]any{"comments": comments})}, completedReviewResponses(verdict)...)
}

func completedReviewResponses(verdict string) []*llm.ChatResponse {
	return []*llm.ChatResponse{toolResponse("task_done", map[string]any{"state": "DONE"}), textResponse(verdict)}
}

func TestReviewReplayConvergesOnPositivelyVerifiedFindings(t *testing.T) {
	repoDir := replayRepository(t)
	comments := []map[string]any{
		authCandidate(),
		authCandidate(),
		candidate("stale cache contract", "medium", "stale authorization is reused", "authorization changes invalidate cached decisions"),
		candidate("optional rename", "low", "name could be clearer", "optional style preference"),
		candidate("contradictory prior claim", "high", "allow is always false", "prior thread assertion"),
	}
	comments[1]["evidence"] = "authorization is never checked"
	for run := 0; run < 5; run++ {
		ordered := append([]map[string]any(nil), comments...)
		ordered = append(ordered[run:], ordered[:run]...)
		var verified []string
		for index, item := range ordered {
			if content := item["content"]; content == "authorization bypass" || content == "stale cache contract" {
				verified = append(verified, fmt.Sprintf("c-%d", index))
			}
		}
		verdict, _ := json.Marshal(verified)
		client := &reviewTestClient{responses: reviewResponses(ordered, string(verdict))}
		findings, agent := runReplay(t, repoDir, client)
		if len(findings) != 3 || findings[0].Evidence != "allow is set without a check" || findings[1].Evidence != "authorization is never checked" || findings[2].Content != "stale cache contract" {
			t.Fatalf("run %d findings = %#v", run, findings)
		}
		if warnings := agent.Warnings(); len(warnings) != 0 {
			t.Fatalf("run %d warnings = %#v", run, warnings)
		}
		if run == 0 {
			request := client.requests[2].Messages[0].ExtractText()
			for _, field := range []string{"failure_mode", "violated_contract", "evidence", "severity"} {
				if !strings.Contains(request, field) {
					t.Fatalf("verifier request missing %s: %s", field, request)
				}
			}
			if strings.Contains(request, "{{current_file_content}}") || !strings.Contains(request, "FULL=package app") {
				t.Fatalf("verifier request missing current source context: %s", request)
			}
		}
	}
}

func TestReviewForcesCandidatePathToCurrentFile(t *testing.T) {
	client := &reviewTestClient{responses: append([]*llm.ChatResponse{
		toolResponse("code_comment", map[string]any{"path": "foreign.go", "comments": []map[string]any{authCandidate()}}),
	}, completedReviewResponses(`["c-0"]`)...)}
	findings, _ := runReplay(t, replayRepository(t), client)
	if len(findings) != 1 || findings[0].Path != "app.go" || len(client.requests) != 3 {
		t.Fatalf("findings = %#v; verifier requests = %d", findings, len(client.requests))
	}
}

func TestReviewVerifierPreservesTemplateTokensInSource(t *testing.T) {
	repoDir := replayRepository(t)
	writeReplayFile(t, repoDir, "app.go", "package app\n\nvar template = \"{{comments}}\"\n")
	claim := authCandidate()
	claim["existing_code"] = `var template = "{{comments}}"`
	client := &reviewTestClient{responses: reviewResponses([]map[string]any{claim}, `["c-0"]`)}

	mustRunAgent(t, newReplayAgent(repoDir, client))
	request := client.requests[2].Messages[0].ExtractText()
	if !strings.Contains(request, `FULL=package app

var template = "{{comments}}"`) {
		t.Fatalf("verifier rewrote a literal source placeholder: %s", request)
	}
}

func TestReviewVerifierRecoversFromOneMalformedResponse(t *testing.T) {
	responses := reviewResponses([]map[string]any{authCandidate()}, "not-json")
	responses = append(responses, textResponse(`["c-0"]`))
	client := &reviewTestClient{responses: responses}

	findings, agent := runReplay(t, replayRepository(t), client)

	if len(findings) != 1 || findings[0].Content != "authorization bypass" {
		t.Fatalf("findings = %#v, want verified finding after corrective retry", findings)
	}
	if warnings := agent.Warnings(); len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want complete verification", warnings)
	}
	if len(client.requests) != 4 {
		t.Fatalf("requests = %d, want main review, completion, and two verifier attempts", len(client.requests))
	}
	retryPrompt := client.requests[3].Messages
	if len(retryPrompt) < 3 || !strings.Contains(retryPrompt[len(retryPrompt)-2].ExtractText(), "not-json") ||
		!strings.Contains(retryPrompt[len(retryPrompt)-1].ExtractText(), "JSON array") {
		t.Fatalf("corrective verifier prompt = %#v", retryPrompt)
	}
}

func TestReviewVerifierCorrectiveAttemptRechecksTokenLimit(t *testing.T) {
	client := &reviewTestClient{responses: reviewResponses(
		[]map[string]any{authCandidate()}, strings.Repeat("invalid ", 9_000),
	)}

	findings, agent := runReplay(t, replayRepository(t), client)
	warnings := agent.Warnings()
	if len(findings) != 0 || len(warnings) != 1 || warnings[0].Type != "verification_incomplete" {
		t.Fatalf("findings = %#v; warnings = %#v", findings, warnings)
	}
	if len(client.requests) != 3 {
		t.Fatalf("requests = %d, want corrective attempt stopped before provider call", len(client.requests))
	}
}

func TestReviewVerifierFailuresAreIncompleteAndFailClosed(t *testing.T) {
	cases := []struct {
		name    string
		verdict string
		error   error
		missing bool
		retry   bool
	}{
		{name: "malformed", verdict: "not-json", retry: true},
		{name: "malformed id", verdict: `["c-0junk"]`, retry: true},
		{name: "null", verdict: `null`, retry: true},
		{name: "error", error: errors.New("verifier unavailable")},
		{name: "missing", missing: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			responses := reviewResponses([]map[string]any{authCandidate()}, test.verdict)
			if test.retry {
				responses = append(responses, textResponse(test.verdict))
			}
			client := &reviewTestClient{responses: responses, errors: map[int]error{2: test.error}}
			agent := newReplayAgent(replayRepository(t), client)
			if test.missing {
				agent.args.Template.ReviewFilterTask = nil
				client.responses = responses[:2]
			}
			findings := mustRunAgent(t, agent)
			warnings := agent.Warnings()
			if len(findings) != 0 || len(warnings) != 1 || warnings[0].Type != "verification_incomplete" {
				t.Fatalf("findings = %#v; warnings = %#v", findings, warnings)
			}
			expectedRequests := 3
			if test.retry {
				expectedRequests = 4
			} else if test.missing {
				expectedRequests = 2
			}
			if len(client.requests) != expectedRequests {
				t.Fatalf("requests = %d, want %d", len(client.requests), expectedRequests)
			}
		})
	}
}

func TestReviewRevalidatesOpenFindingAgainstDelta(t *testing.T) {
	repoDir := replayRepository(t)
	client := &reviewTestClient{responses: completedReviewResponses(`["c-0"]`)}
	agent := newReplayAgent(repoDir, client)
	agent.args.Revalidate = []model.LlmComment{authRevalidationFinding("allow := true")}

	findings := mustRunAgent(t, agent)
	if len(findings) != 1 || findings[0].Content != "authorization bypass" || findings[0].StartLine == 999 {
		t.Fatalf("findings = %#v, want relocated revalidated finding", findings)
	}

	missing := newReplayAgent(repoDir, &reviewTestClient{
		responses: reviewResponses([]map[string]any{authCandidate()}, `["c-0"]`),
	})
	missing.args.Revalidate = []model.LlmComment{{Path: "unchanged.go"}}
	mustRunAgent(t, missing)
	warnings := missing.Warnings()
	if len(warnings) != 1 || warnings[0].Type != "revalidation_incomplete" || warnings[0].File != "unchanged.go" {
		t.Fatalf("warnings = %#v, want missing revalidation target", warnings)
	}
}

func TestReviewRevalidatesPriorFindingOutsideChangedLines(t *testing.T) {
	client := &reviewTestClient{responses: completedReviewResponses(`["c-0"]`)}
	agent := newReplayAgent(replayRepository(t), client)
	agent.args.Revalidate = []model.LlmComment{authRevalidationFinding("var allow = false")}
	findings := mustRunAgent(t, agent)
	prompt := client.requests[1].Messages[0].ExtractText()
	if len(findings) != 1 || findings[0].ExistingCode != "var allow = false" ||
		!strings.Contains(prompt, `"origin":"prior_open"`) || !strings.Contains(prompt, "outside the changed lines") {
		t.Fatalf("findings = %#v, want current-source revalidation outside changed lines", findings)
	}
}

func TestReviewRevalidatesOpenFindingOutsideDelta(t *testing.T) {
	repoDir := replayRepository(t)
	runTestGit(t, repoDir, "commit", "-qm", "reviewed head")
	writeReplayFile(t, repoDir, "other.go", "package app\n\nfunc other() {}\n")
	client := &reviewTestClient{responses: completedReviewResponses(`["c-0"]`)}
	agent := newReplayAgent(repoDir, client)
	openFindings := []model.LlmComment{authRevalidationFinding("allow := true")}
	agent.args.Revalidate = openFindings

	findings := mustRunAgent(t, agent)
	coverage := agent.Coverage()
	if len(findings) != 1 || findings[0].Path != "app.go" || findings[0].StartLine == 999 ||
		len(agent.Warnings()) != 0 || coverage.ChangedFiles != 1 || coverage.ReviewedFiles != 1 || coverage.Status != "complete" {
		t.Fatalf("findings = %#v; warnings = %#v; coverage = %#v", findings, agent.Warnings(), coverage)
	}
	request := client.requests[1].Messages[0].ExtractText()
	if !strings.Contains(request, `"origin":"prior_open"`) || !strings.Contains(request, "allow := true") || strings.Contains(request, "{{verification_scope}}") {
		t.Fatalf("unchanged-finding verifier scope is incomplete: %s", request)
	}

	runTestGit(t, repoDir, "commit", "-qm", "next head")
	var baseline string
	for run := 0; run < 5; run++ {
		sameHead := newReplayAgent(repoDir, &reviewTestClient{responses: []*llm.ChatResponse{textResponse(`["c-0"]`)}})
		sameHead.args.Revalidate = openFindings
		got := mustRunAgent(t, sameHead)
		encoded, _ := json.Marshal(got)
		if run > 0 && string(encoded) != baseline {
			t.Fatalf("same-head run %d = %s, want %s", run, encoded, baseline)
		}
		baseline = string(encoded)
		if len(got) != 1 || sameHead.Coverage().ChangedFiles != 0 || sameHead.Coverage().Status != "complete" {
			t.Fatalf("same-head run %d findings = %#v; coverage = %#v", run, got, sameHead.Coverage())
		}
	}
}

func TestReviewDoesNotSeedRevalidationFindingWhenMainFails(t *testing.T) {
	client := &reviewTestClient{responses: []*llm.ChatResponse{nil}, errors: map[int]error{0: errors.New("main failed")}}
	agent := newReplayAgent(replayRepository(t), client)
	agent.args.Revalidate = []model.LlmComment{{Path: "app.go", Content: "stale claim", ExistingCode: "allow := true"}}

	if _, err := agent.Run(context.Background()); err == nil {
		t.Fatal("expected failed main review")
	}
	if comments := agent.args.CommentCollector.Comments(); len(comments) != 0 {
		t.Fatalf("unverified revalidation findings escaped: %#v", comments)
	}
}

func TestChangedFilteredPathWithOpenFindingIsIncomplete(t *testing.T) {
	repoDir := replayRepository(t)
	writeReplayFile(t, repoDir, "workflow.drawio", "<node/>\n")
	client := &reviewTestClient{responses: []*llm.ChatResponse{
		toolResponse("task_done", map[string]any{"state": "DONE"}),
		textResponse(`["c-0"]`),
	}}
	agent := newReplayAgent(repoDir, client)
	agent.args.Revalidate = []model.LlmComment{{
		Path: "workflow.drawio", Content: "workflow bypass", Severity: "high",
		FailureMode: "workflow validation is bypassed", ViolatedContract: "workflows must be validated",
		Evidence: "the node has no guard", ExistingCode: "<node/>", StartLine: 1, EndLine: 1,
	}}

	findings := mustRunAgent(t, agent)
	warnings := agent.Warnings()
	if len(findings) != 0 || len(warnings) != 1 || warnings[0].Type != "revalidation_incomplete" ||
		agent.Coverage().Status != "incomplete" {
		t.Fatalf("findings = %#v; warnings = %#v; coverage = %#v", findings, warnings, agent.Coverage())
	}
	for _, request := range client.requests {
		if strings.Contains(request.Messages[0].ExtractText(), "unchanged in the rerun delta") {
			t.Fatalf("changed filtered path was given unchanged verification scope")
		}
	}
}

func TestDeletedChangedFileResolvesOpenFindingWithoutFailingCoverage(t *testing.T) {
	repoDir := replayRepository(t)
	runTestGit(t, repoDir, "rm", "-f", "app.go")
	agent := newReplayAgent(repoDir, &reviewTestClient{})
	agent.args.Revalidate = []model.LlmComment{authRevalidationFinding("var allow = false")}

	findings := mustRunAgent(t, agent)
	coverage := agent.Coverage()
	if len(findings) != 0 || len(agent.Warnings()) != 0 || coverage.Status != "complete" ||
		coverage.ChangedFiles != 1 || coverage.EligibleFiles != 0 || coverage.ReviewedFiles != 0 ||
		coverage.ExcludedFiles != 1 || len(coverage.Files) != 1 || coverage.Files[0].ExcludeReason != ExcludeDeleted {
		t.Fatalf("findings = %#v; warnings = %#v; coverage = %#v", findings, agent.Warnings(), coverage)
	}
}

func TestFailedFileCannotLeakCandidateWhenAnotherFileSucceeds(t *testing.T) {
	repoDir := replayRepository(t)
	writeReplayFile(t, repoDir, "other.go", "package app\n\nfunc other() {}\n")
	client := &reviewTestClient{responses: []*llm.ChatResponse{
		toolResponse("code_comment", map[string]any{"comments": []map[string]any{authCandidate()}}),
		textResponse("no completion"),
		toolResponse("task_done", map[string]any{"state": "DONE"}),
	}}

	findings := mustRunAgent(t, newReplayAgent(repoDir, client))
	if len(findings) != 0 {
		t.Fatalf("failed file leaked unverified findings: %#v", findings)
	}
}

func TestReviewRequiresExplicitSuccessfulCompletion(t *testing.T) {
	for _, test := range []struct {
		name      string
		responses []*llm.ChatResponse
	}{
		{name: "no task done", responses: []*llm.ChatResponse{textResponse("analysis only"), textResponse("still analyzing")}},
		{name: "task failed", responses: []*llm.ChatResponse{toolResponse("task_done", map[string]any{"state": "FAILED"})}},
		{name: "malformed sibling", responses: []*llm.ChatResponse{toolCallsResponse(
			toolCall("code_comment", map[string]any{"comments": []any{42}}),
			toolCall("task_done", map[string]any{"state": "DONE"}),
		)}},
		{name: "malformed prior round", responses: []*llm.ChatResponse{
			toolResponse("code_comment", map[string]any{"comments": []any{42}}),
			toolResponse("task_done", map[string]any{"state": "DONE"}),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent := newReplayAgent(replayRepository(t), &reviewTestClient{responses: test.responses})
			findings, err := agent.Run(context.Background())
			if err == nil || len(findings) != 0 || agent.Coverage().Status != "incomplete" {
				t.Fatalf("findings = %#v; error = %v; coverage = %#v", findings, err, agent.Coverage())
			}
		})
	}
}

func TestUnsupportedChangedFileIsInventoriedWithoutFailingCoverage(t *testing.T) {
	repoDir := replayRepository(t)
	writeReplayFile(t, repoDir, "workflow.drawio", "diagram")
	_, agent := runReplay(t, repoDir, &reviewTestClient{
		responses: reviewResponses([]map[string]any{authCandidate()}, `["c-0"]`),
	})
	warnings := agent.Warnings()
	coverage := agent.Coverage()
	if len(warnings) != 0 || coverage.Status != "complete" || coverage.ChangedFiles != 2 ||
		coverage.EligibleFiles != 1 || coverage.ReviewedFiles != 1 || coverage.ExcludedFiles != 1 ||
		len(coverage.Files) != 2 {
		t.Fatalf("warnings = %#v; coverage = %#v", warnings, coverage)
	}
}

func TestAllSupportedFilesExcludedByTokenLimitAreIncomplete(t *testing.T) {
	agent := newReplayAgent(replayRepository(t), &reviewTestClient{})
	agent.args.Template.MaxTokens = 2
	findings := mustRunAgent(t, agent)
	coverage := agent.Coverage()
	if len(findings) != 0 || agent.FilesReviewed() != 0 || coverage.Status != "incomplete" ||
		coverage.EligibleFiles != 1 || coverage.ReviewedFiles != 0 || len(agent.Warnings()) != 1 {
		t.Fatalf("findings = %#v; warnings = %#v; coverage = %#v", findings, agent.Warnings(), coverage)
	}
}

func runReplay(t *testing.T, repoDir string, client llm.LLMClient) ([]model.LlmComment, *Agent) {
	t.Helper()
	agent := newReplayAgent(repoDir, client)
	return mustRunAgent(t, agent), agent
}

func mustRunAgent(t *testing.T, agent *Agent) []model.LlmComment {
	t.Helper()
	findings, err := agent.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return findings
}

func newReplayAgent(repoDir string, client llm.LLMClient) *Agent {
	collector := tool.NewCommentCollector()
	registry := tool.NewRegistry()
	registry.Register(&tool.CodeCommentProvider{Collector: collector})
	registry.Register(tool.NewFileRead(&tool.FileReader{RepoDir: repoDir, Mode: tool.ModeWorkspace}))
	return New(Args{
		RepoDir: repoDir, LLMClient: client, Model: "recorded-reviewer", Tools: registry,
		CommentCollector: collector, MaxConcurrency: 1,
		Template: template.Template{
			MainTask: template.LlmConversation{Messages: []template.ChatMessage{{
				Role: "user", Content: "{{diff}} {{requirement_background}} {{plan_guidance}}",
			}}},
			ReviewFilterTask: &template.LlmConversation{Messages: []template.ChatMessage{{
				Role: "user", Content: "SCOPE={{verification_scope}} FULL={{current_file_content}} RULE={{system_rule}} BACKGROUND={{requirement_background}} {{diff}} {{comments}}",
			}}},
			MaxTokens: 10_000, MaxToolRequestTimes: 2,
		},
	})
}

func replayRepository(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	runTestGit(t, repoDir, "init", "-q")
	runTestGit(t, repoDir, "config", "user.email", "review@example.com")
	runTestGit(t, repoDir, "config", "user.name", "Review Test")
	writeReplayFile(t, repoDir, "app.go", "package app\n\nvar allow = false\n")
	runTestGit(t, repoDir, "commit", "-qm", "base")
	writeReplayFile(t, repoDir, "app.go", "package app\n\nvar allow = false\n\nfunc changed() {\n\tallow := true\n\t_ = allow\n}\n")
	return repoDir
}

func writeReplayFile(t *testing.T, repoDir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repoDir, "add", name)
}

func runTestGit(t *testing.T, repoDir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
