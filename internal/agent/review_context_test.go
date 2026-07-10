package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/open-code-review/open-code-review/internal/config/template"
	"github.com/open-code-review/open-code-review/internal/llm"
	"github.com/open-code-review/open-code-review/internal/model"
)

type reviewContextCaptureClient struct {
	requests  []llm.ChatRequest
	responses []string
}

func (c *reviewContextCaptureClient) CompletionsWithCtx(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	c.requests = append(c.requests, req)
	content := "done"
	if len(c.responses) >= len(c.requests) {
		content = c.responses[len(c.requests)-1]
	}
	return &llm.ChatResponse{
		Choices: []llm.Choice{{
			Message: llm.ResponseMessage{
				Content: &content,
				ToolCalls: []llm.ToolCall{{
					ID: "done",
					Function: llm.FunctionCall{
						Name:      "task_done",
						Arguments: "{}",
					},
				}},
			},
		}},
	}, nil
}

func mainTaskTemplate(content string, maxTokens int) template.Template {
	return template.Template{
		MainTask: template.LlmConversation{
			Messages: []template.ChatMessage{{
				Role:    "user",
				Content: content,
			}},
		},
		MaxTokens:           maxTokens,
		MaxToolRequestTimes: 1,
	}
}

func setPlanTask(agent *Agent, content string) {
	agent.args.Template.PlanTask = &template.LlmConversation{
		Messages: []template.ChatMessage{{Role: "user", Content: content}},
	}
}

func newCapturedReviewContextAgent(background, reviewContext string, tpl template.Template) (*Agent, *reviewContextCaptureClient) {
	client := &reviewContextCaptureClient{}
	agent := New(Args{
		Background: background,
		LLMClient:  client,
		Model:      "test-model",
		ReviewContext: map[string]string{
			"src/app.go": reviewContext,
		},
		Template: tpl,
	})
	return agent, client
}

func executeReviewContextSubtask(t *testing.T, agent *Agent, client *reviewContextCaptureClient, wantRequests int) {
	t.Helper()
	err := agent.executeSubtask(context.Background(), model.Diff{
		NewPath:    "src/app.go",
		Diff:       "@@\n+change",
		Insertions: 1,
	})
	if err != nil {
		t.Fatalf("executeSubtask: %v", err)
	}
	if len(client.requests) != wantRequests {
		t.Fatalf("LLM requests = %d, want %d", len(client.requests), wantRequests)
	}
}

func capturedRequestText(t *testing.T, client *reviewContextCaptureClient, index int) string {
	t.Helper()
	if index >= len(client.requests) || len(client.requests[index].Messages) == 0 {
		t.Fatalf("missing captured request %d in %#v", index, client.requests)
	}
	return client.requests[index].Messages[0].ExtractText()
}

func TestRequirementBackgroundInjectsOnlyCurrentFileReviewContext(t *testing.T) {
	agent := New(Args{
		Background: "existing background",
		ReviewContext: map[string]string{
			"src/app.go":   "prior app context",
			"src/other.go": "prior other context",
		},
	})

	background := agent.requirementBackground("src/app.go")

	if !strings.Contains(background, "existing background") {
		t.Fatalf("existing background missing:\n%s", background)
	}
	if !strings.Contains(background, "prior app context") {
		t.Fatalf("current file context missing:\n%s", background)
	}
	if strings.Contains(background, "prior other context") {
		t.Fatalf("other file context leaked:\n%s", background)
	}
}

func TestRequirementBackgroundNoContextMatchesExistingBackground(t *testing.T) {
	withoutContext := New(Args{Background: "existing background"}).
		requirementBackground("src/app.go")
	withUnmatchedContext := New(Args{
		Background: "existing background",
		ReviewContext: map[string]string{
			"src/other.go": "prior other context",
		},
	}).requirementBackground("src/app.go")

	if withoutContext != withUnmatchedContext {
		t.Fatalf("no-context background changed\nwithout:\n%s\nwith unmatched:\n%s", withoutContext, withUnmatchedContext)
	}
	if strings.Contains(withoutContext, "Prior unresolved review thread context") {
		t.Fatalf("no-context background contains context header:\n%s", withoutContext)
	}
}

func TestMainTaskReviewContextDoesNotExpandPlanGuidancePlaceholder(t *testing.T) {
	agent, client := newCapturedReviewContextAgent(
		"",
		"literal {{plan_guidance}} marker",
		mainTaskTemplate(
			"Context:\n{{requirement_background}}\nGuidance:\n{{plan_guidance}}",
			10000,
		),
	)

	executeReviewContextSubtask(t, agent, client, 1)
	rendered := capturedRequestText(t, client, 0)
	if !strings.Contains(rendered, "literal {{plan_guidance}} marker") {
		t.Fatalf("review context placeholder was expanded:\n%s", rendered)
	}
}

func TestPlanTaskReviewContextDoesNotExpandPlanToolsPlaceholder(t *testing.T) {
	agent, client := newCapturedReviewContextAgent(
		"",
		"literal {{plan_tools}} marker",
		mainTaskTemplate("{{requirement_background}}\n{{plan_guidance}}", 10000),
	)
	agent.args.PlanToolDefs = []llm.ToolDef{{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "plan_tool",
			Description: "plan helper",
			Parameters:  map[string]any{},
		},
	}}
	agent.args.Template.PlanTask = &template.LlmConversation{
		Messages: []template.ChatMessage{{
			Role:    "user",
			Content: "Context:\n{{requirement_background}}\nTools:\n{{plan_tools}}",
		}},
	}

	executeReviewContextSubtask(t, agent, client, 2)
	renderedPlan := capturedRequestText(t, client, 0)
	if !strings.Contains(renderedPlan, "literal {{plan_tools}} marker") {
		t.Fatalf("review context plan_tools marker was expanded:\n%s", renderedPlan)
	}
}

func TestPlanTaskDiffDoesNotExpandRequirementBackgroundPlaceholder(t *testing.T) {
	agent, client := newCapturedReviewContextAgent(
		"background",
		"",
		mainTaskTemplate("main", 10000),
	)
	setPlanTask(agent, "Diff:\n{{diff}}\nContext:\n{{requirement_background}}")

	err := agent.executeSubtask(context.Background(), model.Diff{
		NewPath:    "src/app.go",
		Diff:       "@@\n+literal {{requirement_background}} marker",
		Insertions: 1,
	})
	if err != nil {
		t.Fatalf("executeSubtask: %v", err)
	}
	rendered := capturedRequestText(t, client, 0)
	if !strings.Contains(rendered, "literal {{requirement_background}} marker") {
		t.Fatalf("diff placeholder was expanded:\n%s", rendered)
	}
}

func TestGeneratedPlanMarkersRemainLiteral(t *testing.T) {
	agent, client := newCapturedReviewContextAgent(
		"background",
		"",
		mainTaskTemplate("Context: {{requirement_background}}\nPlan: {{plan_guidance}}", 10000),
	)
	client.responses = []string{"literal {{requirement_background}} marker", "done"}
	setPlanTask(agent, "plan")

	executeReviewContextSubtask(t, agent, client, 2)
	rendered := capturedRequestText(t, client, 1)
	if !strings.Contains(rendered, "literal {{requirement_background}} marker") {
		t.Fatalf("generated plan marker was expanded:\n%s", rendered)
	}
}

func TestOversizedReviewContextIsOmittedBeforeTokenSkip(t *testing.T) {
	agent, client := newCapturedReviewContextAgent(
		"existing background",
		strings.Repeat("review-context ", 1000),
		mainTaskTemplate("File {{current_file_path}}\n{{diff}}\n{{requirement_background}}", 100),
	)

	executeReviewContextSubtask(t, agent, client, 1)
	rendered := capturedRequestText(t, client, 0)
	if strings.Contains(rendered, "review-context") {
		t.Fatalf("oversized review context was not omitted:\n%s", rendered)
	}
	if !strings.Contains(rendered, "existing background") {
		t.Fatalf("existing background missing after context omission:\n%s", rendered)
	}
	warnings := agent.Warnings()
	if len(warnings) != 1 || warnings[0].Type != "review_context_omitted_token_budget" {
		t.Fatalf("warnings = %#v, want review context omission warning", warnings)
	}
}

func TestOverBudgetPlanDoesNotReachProvider(t *testing.T) {
	agent, client := newCapturedReviewContextAgent(
		"",
		strings.Repeat("review-context ", 1000),
		mainTaskTemplate("main", 100),
	)
	setPlanTask(agent, "{{requirement_background}}")

	executeReviewContextSubtask(t, agent, client, 1)
	if got := capturedRequestText(t, client, 0); got != "main" {
		t.Fatalf("only provider request = %q, want main task", got)
	}
}
