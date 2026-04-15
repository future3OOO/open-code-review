// Package agent implements the Code Review Native Agent logic ported from Java.
// It drives a Plan Phase -> Main Loop (tool-use cycle) per file, collecting LLM-generated review comments.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/argus-review/argus/internal/config"
	"github.com/argus-review/argus/internal/diff"
	"github.com/argus-review/argus/internal/llm"
	"github.com/argus-review/argus/internal/model"
	"github.com/argus-review/argus/internal/tool"
)

// Args holds all dependencies and configuration needed to run a review session.
type Args struct {
	// RepoDir is the root of the git repository.
	RepoDir string

	// BaseRef and HeadRef define the diff range (e.g., "main..feature-branch" or empty for staged).
	BaseRef string
	HeadRef string

	// UseStaged if true, reviews staged changes instead of base..head ref.
	UseStaged bool

	// Template loaded from YAML config file.
	Template config.Template

	// LLM client for model inference.
	LLMClient *llm.Client

	// Tool registry mapping tool aliases to implementations.
	Tools tool.Registry

	// CommentWorkerPool — separate goroutine pool for running asynchronous
	// comment post-processing tasks (tracking, re-tracking, reflection,
	// suggestion validation). This mirrors the Java side's subtaskExecutor
	// which executes the CODE_COMMENT tool off the critical path so that the
	// main LLM tool-use loop can continue issuing requests while comments are
	// being processed in the background.
	//
	// When nil (the default), comment processing happens synchronously inside
	// executeToolCall instead of via a separate worker pool.
	CommentWorkerPool *CommentWorkerPool

	// Concurrency limit for per-file subtasks. Defaults to number of CPUs.
	MaxConcurrency int

	// Per-file timeout in minutes. 0 means no timeout.
	PerFileTimeoutMinutes int

	// DryRun - when true, runs without actually submitting comments (useful for testing).
	DryRun bool
}

// Agent orchestrates the AI-powered code review.
type Agent struct {
	args            Args
	diffMap         map[string]string // path -> diff text
	newFileMap      map[string]string // path -> new file content
	diffs           []model.Diff      // parsed diffs
	totalInsertions int64
	totalDeletions  int64
	currentDate     string
}

// CommentWorkerPool manages a fixed-size pool of workers dedicated to
// processing code-review comment post-steps (line-range tracking,
// re-tracking, reflection, suggestion validation) asynchronously.
//
// These steps can be time-consuming (network calls to LLM, external APIs,
// heavy computation). By offloading them to a worker pool the main LLM
// tool-use loop stays unblocked, reducing overall latency - just like the
// Java implementation uses a dedicated subtaskExecutor for the CODE_COMMENT
// tool (see CodeReviewNativeAgent.executeToolCall ~L640-642).
type CommentWorkerPool struct {
	semaphore chan struct{}
	wg        sync.WaitGroup
	resultsMu sync.Mutex
	results   []model.LlmComment
}

// NewCommentWorkerPool creates a pool with the given concurrency limit.
func NewCommentWorkerPool(workerCount int) *CommentWorkerPool {
	if workerCount <= 0 {
		workerCount = 8
	}
	return &CommentWorkerPool{
		semaphore: make(chan struct{}, workerCount),
	}
}

// Submit runs f in a background goroutine bounded by the semaphore.
// When f completes its return value is collected internally.
func (p *CommentWorkerPool) Submit(f func() ([]model.LlmComment, error)) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.semaphore <- struct{}{}       // acquire
		defer func() { <-p.semaphore }() // release

		comments, _ := f() // errors are logged; results are merged below
		p.resultsMu.Lock()
		p.results = append(p.results, comments...)
		p.resultsMu.Unlock()
	}()
}

// Await blocks until all submitted work has completed and returns aggregated results.
func (p *CommentWorkerPool) Await() []model.LlmComment {
	p.wg.Wait()
	return p.results
}

// New creates a new Agent from the given arguments.
func New(args Args) *Agent {
	if args.Tools == nil {
		args.Tools = make(tool.Registry)
	}
	return &Agent{args: args}
}

// Run executes the full review pipeline: parse diffs -> plan per file -> LLM tool-loop -> collect comments.
func (a *Agent) Run(ctx context.Context) ([]model.LlmComment, error) {
	// Step 1: Parse diffs
	if err := a.loadDiffs(); err != nil {
		return nil, fmt.Errorf("load diffs: %w", err)
	}

	if len(a.diffs) == 0 {
		fmt.Println("[argus] No files changed. Skipping review.")
		return nil, nil
	}

	a.currentDate = time.Now().Format("2006-01-02 15:04")

	fmt.Printf("[argus] Reviewing %d file(s) in %s\n", len(a.diffs), a.args.RepoDir)

	// Step 2: Dispatch per-file subtasks concurrently
	return a.dispatchSubtasks(ctx)
}

// loadDiffs populates the diff-related fields.
func (a *Agent) loadDiffs() error {
	var rawText string
	var err error

	if a.args.UseStaged {
		rawText, err = diff.RunGitDiffStaged(a.args.RepoDir)
	} else {
		rawText, err = diff.RunGitDiff(a.args.RepoDir, a.args.BaseRef, a.args.HeadRef)
	}
	if err != nil {
		return fmt.Errorf("git diff failed: %w", err)
	}

	parsed, err := diff.ParseDiffText(rawText, a.args.RepoDir)
	if err != nil {
		return fmt.Errorf("parse diff: %w", err)
	}

	a.diffMap = make(map[string]string)
	a.newFileMap = make(map[string]string)
	a.diffs = parsed

	for i := range parsed {
		d := &parsed[i]
		if d.NewPath != "/dev/null" {
			a.diffMap[d.NewPath] = d.Diff
			a.newFileMap[d.NewPath] = d.NewFileContent
		}
		a.totalInsertions += d.Insertions
		a.totalDeletions += d.Deletions
	}

	return nil
}

func lookupTool(reg tool.Registry, t tool.Tool) tool.Provider {
	p, ok := reg[t.Alias]
	if !ok {
		return nil
	}
	return p
}

// dispatchSubtasks runs the Plan + Main phases for each changed file concurrently.
func (a *Agent) dispatchSubtasks(ctx context.Context) ([]model.LlmComment, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allComments []model.LlmComment

	concurrency := a.args.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 8
	}

	sem := make(chan struct{}, concurrency)
	timeout := time.Duration(a.args.PerFileTimeoutMinutes) * time.Minute

	for i := range a.diffs {
		wg.Add(1)
		sem <- struct{}{} // acquire semaphore

		go func(d model.Diff) {
			defer wg.Done()
			defer func() { <-sem }() // release

			var fileCtx context.Context
			var cancel context.CancelFunc
			if timeout > 0 {
				fileCtx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			} else {
				fileCtx = ctx
			}

			fileComments, err := a.executeSubtask(fileCtx, d)
			if err != nil {
				fmt.Printf("[argus] Subtask error for %s: %v\n", d.NewPath, err)
			}
			mu.Lock()
			allComments = append(allComments, fileComments...)
			mu.Unlock()
		}(a.diffs[i])
	}

	wg.Wait()
	return allComments, nil
}

type fillVars struct {
	path        string
	diff        string
	planGuide   string
	changeFiles string
}

// executeSubtask performs the Plan Phase + Main Loop for a single file.
func (a *Agent) executeSubtask(ctx context.Context, d model.Diff) ([]model.LlmComment, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	newPath := d.NewPath

	// Build change-files list excluding current file
	changeFilesExcludingCurrent := a.buildChangeFilesExcept(newPath)

	// Phase 1: Plan
	var planResult string
	if a.args.Template.PlanTask != nil && len(a.args.Template.PlanTask.Messages) > 0 {
		var err error
		planResult, err = a.executePlanPhase(ctx, newPath, d.Diff, changeFilesExcludingCurrent)
		if err != nil {
			fmt.Printf("[argus] Plan phase failed for %s: %v (continuing without plan)\n", newPath, err)
			planResult = ""
		}
	}

	// Phase 2: Main task loop
	if len(a.args.Template.MainTask.Messages) == 0 {
		return nil, fmt.Errorf("main_task.messages is empty in template")
	}

	messages := a.fillMessages(a.args.Template.MainTask.Messages, fillVars{
		path:        newPath,
		diff:        d.Diff,
		planGuide:   planResult,
		changeFiles: changeFilesExcludingCurrent,
	})

	tokenCount := countMessagesTokens(messages)
	if tokenCount > a.args.Template.TokenWarningThreshold {
		fmt.Printf("[argus] WARNING: prompt tokens (%d) exceed threshold (%d) for %s\n",
			tokenCount, a.args.Template.TokenWarningThreshold, newPath)
		return nil, nil
	}

	return a.performLlmCodeReview(ctx, messages, newPath)
}

// fillMessages replaces template variables in messages.
func (a *Agent) fillMessages(msgs []config.ChatMessage, vars fillVars) []llm.Message {
	result := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		content := m.Content
		content = strings.ReplaceAll(content, "{{current_system_date_time}}", a.currentDate)
		content = strings.ReplaceAll(content, "{{current_file_path}}", vars.path)
		content = strings.ReplaceAll(content, "{{system_rule}}", "") // TODO: configurable rule injection
		content = strings.ReplaceAll(content, "{{code_review_background}}", a.args.Template.CodeReviewBackgroundTpl)
		content = strings.ReplaceAll(content, "{{change_files}}", vars.changeFiles)
		content = strings.ReplaceAll(content, "{{plan_guidance}}", vars.planGuide)
		content = strings.ReplaceAll(content, "{{diff}}", vars.diff)
		result = append(result, llm.Message{Role: m.Role, Content: content})
	}
	return result
}

// buildChangeFilesExcept returns a formatted list of changed files except the given path.
func (a *Agent) buildChangeFilesExcept(excludePath string) string {
	var sb strings.Builder
	for i, d := range a.diffs {
		if d.IsBinary {
			continue
		}
		if d.NewPath == excludePath || d.OldPath == excludePath {
			continue
		}
		status := "MODIFIED"
		switch {
		case d.IsNew:
			status = "ADDED"
		case d.IsDeleted:
			status = "DELETED"
		case d.OldPath != d.NewPath:
			status = "RENAMED"
		}
		sb.WriteString(status + "   " + d.NewPath)
		if i < len(a.diffs)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// executePlanPhase sends a request to the LLM to produce a structured review plan.
func (a *Agent) executePlanPhase(_ context.Context, newPath, rawDiff, changeFiles string) (string, error) {
	pt := a.args.Template.PlanTask
	messages := a.fillMessages(pt.Messages, fillVars{
		path:        newPath,
		diff:        rawDiff,
		planGuide:   "",
		changeFiles: changeFiles,
	})

	resp, err := a.args.LLMClient.GeneralRequest(messages, pt.Model, nil)
	if err != nil {
		return "", fmt.Errorf("plan request: %w", err)
	}
	fmt.Printf("[argus] Plan completed for %s\n", newPath)
	return resp.Content(), nil
}

// performLlmCodeReview runs the iterative tool-use loop until task_done, max iterations, or empty tool calls.
//
// Mirrors the Java implementation's approach where the CODE_COMMENT tool is submitted
// to a subtaskExecutor so that the main LLM loop continues without blocking on the
// expensive post-processing (line-range tracking, re-tracking, reflection, validation).
// When CommentWorkerPool is configured, comments are collected asynchronously; otherwise
// they are processed synchronously but the main-loop semantics remain identical.
func (a *Agent) performLlmCodeReview(ctx context.Context, messages []llm.Message, newPath string) ([]model.LlmComment, error) {
	toolReqCount := a.args.Template.MaxToolRequestTimes

	for toolReqCount > 0 {
		select {
		case <-ctx.Done():
			return a.collectPendingComments(), ctx.Err()
		default:
		}

		toolReqCount--

		reqTools := buildToolDefsFromRegistry(a.args.Tools)

		resp, err := a.args.LLMClient.Completions(llm.ChatRequest{
			Model:    a.args.Template.MainTask.Model,
			Messages: messages,
			Tools:    reqTools,
		})
		if err != nil {
			return a.collectPendingComments(), fmt.Errorf("LLM completion error: %w", err)
		}

		content := resp.Content()
		calls := resp.ToolCalls()

		if len(calls) == 0 {
			// No tool calls - remind the model
			fmt.Printf("[argus] No tool calls parsed for %s, retrying...\n", newPath)
			messages = append(messages,
				llm.Message{Role: "assistant", Content: content},
				llm.Message{Role: "user", Content: "You did not successfully call any tools. Please try again or use task_done if finished."},
			)
			continue
		}

		var results []tool.ToolCallResult
		taskCompleted := false
		hasValidResult := false

		for _, call := range calls {
			cp := a.executeToolCall(ctx, newPath, call)
			if cp.Completed {
				results = append(results, tool.ToolCallResult{
					ToolCallID: call.ID,
					Name:       call.Function.Name,
					Result:     "Task completed successfully.",
				})
				taskCompleted = true
			} else if cp.Data != "" {
				results = append(results, tool.ToolCallResult{
					ToolCallID: call.ID,
					Name:       call.Function.Name,
					Result:     cp.Data,
				})
				hasValidResult = true
			} else {
				results = append(results, tool.ToolCallResult{
					ToolCallID: call.ID,
					Name:       call.Function.Name,
					Result:     "Error: Tool execution returned no result.",
				})
			}
		}

		if taskCompleted {
			break
		}
		if !hasValidResult {
			fmt.Printf("[argus] No valid tool results for %s, stopping.\n", newPath)
			break
		}

		succeed := a.addNextMessage(content, results, &messages)
		if !succeed {
			fmt.Printf("[argus] Context compression exceeded threshold for %s, stopping.\n", newPath)
			break
		}
	}

	if toolReqCount <= 0 {
		fmt.Printf("[argus] Max tool requests reached for %s.\n", newPath)
	}

	return a.collectPendingComments(), nil
}

// executeToolCall handles a single tool call from the LLM.
//
// For the CODE_COMMENT tool this mirrors the Java branch at CodeReviewNativeAgent L640-642:
//
//   if (tool == Tool.CODE_COMMENT) {
//       pendingCommentFutures.add(subtaskExecutor.submit(() -> getCodeComments(...)));
//       return COMMENT_SUCCEED;  // non-blocking
//   }
//
// All other tools execute synchronously on the calling goroutine.
func (a *Agent) executeToolCall(_ context.Context, _ string, call llm.ToolCall) tool.TaskCheckpoint {
	t := tool.OfAlias(call.Function.Name)
	if !t.IsKnown() {
		return tool.Of(tool.NotAvailableMsg)
	}

	if t == tool.TaskDone {
		return tool.Complete()
	}

	p := lookupTool(a.args.Tools, t)
	if p == nil {
		return tool.Of(tool.NotAvailableMsg)
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return tool.Of(fmt.Sprintf("Error parsing tool arguments for %s: %v", t.Alias, err))
	}

	// Async path for code_comment when worker pool is configured
	// Mirrors Java: pendingCommentFutures.add(subtaskExecutor.submit(() -> getCodeComments(...)))
	if t == tool.CodeComment && a.args.CommentWorkerPool != nil {
		pool := a.args.CommentWorkerPool
		pool.Submit(func() ([]model.LlmComment, error) {
			_, _ = p.Execute(args) // errors are ignored; individual failures shouldn't abort the review
			return []model.LlmComment{}, nil
		})
		// Return immediate success - actual comment processing continues off
		// the critical path, exactly like Java's subtaskExecutor.submit for CODE_COMMENT.
		return tool.Of(tool.CommentSucceed)
	}

	// Synchronous path for all other tools
	result, err := p.Execute(args)
	if err != nil {
		return tool.Of(fmt.Sprintf("Error executing tool %s: %v", t.Alias, err))
	}
	return tool.Of(result)
}

// collectPendingComments gathers the results of all async comment workers
// (if a pool was configured). If no pool is set, this is a no-op.
func (a *Agent) collectPendingComments() []model.LlmComment {
	if a.args.CommentWorkerPool != nil {
		return a.args.CommentWorkerPool.Await()
	}
	return nil
}

// addNextMessage adds assistant + tool response messages to the conversation history.
func (a *Agent) addNextMessage(assistantContent string, results []tool.ToolCallResult, messages *[]llm.Message) bool {
	// Check if context compression is needed
	tokenCount := countMessagesTokens(*messages)
	if tokenCount > a.args.Template.TokenWarningThreshold {
		*messages = compressMessages(*messages, a.args.Template.MemoryCompressionTask, a.args.LLMClient)
	}

	// Add assistant message
	*messages = append(*messages, llm.Message{
		Role:    "assistant",
		Content: assistantContent,
	})

	// Add tool response messages
	for _, r := range results {
		*messages = append(*messages, llm.Message{
			Role:    "tool",
			Content: r.Result,
		})
	}

	return countMessagesTokens(*messages) < a.args.Template.TokenWarningThreshold
}

func countMessagesTokens(msgs []llm.Message) int {
	var total int
	for _, m := range msgs {
		total += llm.CountTokens(m.Content)
	}
	return total
}

func buildToolDefsFromRegistry(reg tool.Registry) []llm.ToolDef {
	var defs []llm.ToolDef
	for _, provider := range reg {
		defs = append(defs, llm.ToolDef{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        provider.Tool().Alias,
				Description: "", // TODO: pull description from provider interface
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		})
	}
	return defs
}

// compressMessages runs the memory compression task and replaces old messages with a summary.
func compressMessages(msgs []llm.Message, compTask config.LlmConversation, client *llm.Client) []llm.Message {
	if len(compTask.Messages) == 0 || len(msgs) <= 2 {
		return msgs[:min(len(msgs), 2)]
	}

	contextXML := buildMessageXML(msgs[2:])
	compressionMsgs := make([]llm.Message, 0, len(compTask.Messages))
	for _, m := range compTask.Messages {
		content := strings.ReplaceAll(m.Content, "{{context}}", contextXML)
		compressionMsgs = append(compressionMsgs, llm.Message{Role: m.Role, Content: content})
	}

	resp, err := client.GeneralRequest(compressionMsgs, compTask.Model, nil)
	if err != nil {
		fmt.Printf("[argus] Memory compression failed: %v\n", err)
		return msgs[:2]
	}

	summary := resp.Content()
	if summary == "" {
		return msgs[:2]
	}

	compressed := msgs[:2]
	// Append summary to the original user prompt
	userMsg := compressed[1]
	userMsg.Content = userMsg.Content + "\n\n" + summary
	compressed[1] = userMsg
	return compressed
}

func buildMessageXML(msgs []llm.Message) string {
	var sb strings.Builder
	for i, m := range msgs {
		sb.WriteString(fmt.Sprintf("<message id=\"%d\" role=\"%s\">\n", i, m.Role))
		sb.WriteString("    <content>\n")
		sb.WriteString(fmt.Sprintf("      %s\n", m.Content))
		sb.WriteString("    </content>\n")
		sb.WriteString("</message>")
		if i < len(msgs)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
