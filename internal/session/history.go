// Package session provides a session history mechanism for collecting conversation
// records during code review task execution. It organizes records by file path
// and request type (plan_task, main_task, memory_compression_task).
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/open-code-review/open-code-review/internal/llm"
)

// TaskType identifies the kind of LLM request within a file subtask.
type TaskType string

const (
	PlanTask             TaskType = "plan_task"
	MainTask             TaskType = "main_task"
	MemoryCompressionTask TaskType = "memory_compression_task"
)

// SessionHistory is the top-level container for an entire CR run.
// It is safe for concurrent use by multiple goroutines.
type SessionHistory struct {
	mu        sync.Mutex
	SessionID string
	RepoDir   string
	StartTime time.Time
	EndTime   time.Time
	Debug     bool
	FileSessions map[string]*FileSession
}

// FileSession represents the conversation records for a single file subtask.
type FileSession struct {
	mu          sync.Mutex
	FilePath    string
	TaskRecords map[TaskType][]*TaskRecord
}

// TaskRecord captures a single LLM request-response cycle within a file subtask.
type TaskRecord struct {
	Type        TaskType
	RequestNo   int                // sequential number within this task type
	RequestMessages []llm.Message  // messages sent to LLM
	Response      *ResponseRecord
	ToolResults   []ToolResultRecord
	Duration      time.Duration
	Error         string
}

// TokenUsage holds estimated token usage for a single LLM request/response cycle.
// Calculated locally using tiktoken so it works with any model provider.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

// ResponseRecord holds the parsed LLM response.
type ResponseRecord struct {
	Content   string
	ToolCalls []llm.ToolCall
	Model     string
	Usage     *TokenUsage
}

// ToolResultRecord records the result of a tool call executed after the LLM response.
type ToolResultRecord struct {
	ToolName  string
	Arguments string
	Result    string
}

// New creates a new SessionHistory with the given session ID and repo directory.
func New(sessionID, repoDir string) *SessionHistory {
	return &SessionHistory{
		SessionID:  sessionID,
		RepoDir:    repoDir,
		StartTime:  time.Now(),
		FileSessions: make(map[string]*FileSession),
	}
}

// GetOrCreateFileSession returns the FileSession for the given file path,
// creating one if it doesn't exist yet.
func (sh *SessionHistory) GetOrCreateFileSession(filePath string) *FileSession {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	fs, ok := sh.FileSessions[filePath]
	if !ok {
		fs = &FileSession{
			FilePath:    filePath,
			TaskRecords: make(map[TaskType][]*TaskRecord),
		}
		sh.FileSessions[filePath] = fs
	}
	return fs
}

// Finalize marks the session as complete and sets the end time.
func (sh *SessionHistory) Finalize() {
	sh.mu.Lock()
	sh.EndTime = time.Now()
	debug := sh.Debug
	sh.mu.Unlock()
	if debug {
		sh.writeDebugDump()
	}
}

// --- Debug Dump ---

type sessionSnapshot struct {
	SessionID    string                   `json:"session_id"`
	RepoDir      string                   `json:"repo_dir"`
	StartTime    time.Time                `json:"start_time"`
	EndTime      time.Time                `json:"end_time"`
	FileSessions []*fileSessionSnapshot   `json:"file_sessions"`
}

type fileSessionSnapshot struct {
	FilePath    string                                 `json:"file_path"`
	TaskRecords map[string][]*taskRecordSnapshot       `json:"task_records"`
}

type taskRecordSnapshot struct {
	Type            string             `json:"type"`
	RequestNo       int                `json:"request_no"`
	RequestMessages any                `json:"request_messages"`
	Response        *ResponseRecord    `json:"response"`
	ToolResults     []ToolResultRecord `json:"tool_results"`
	Duration        time.Duration      `json:"duration"`
	Error           string             `json:"error"`
}

func (sh *SessionHistory) writeDebugDump() {
	sessionName := sh.SessionID
	if sessionName == "" {
		sessionName = fmt.Sprintf("unknown-%d", time.Now().Unix())
	}

	sh.mu.Lock()
	snap := &sessionSnapshot{
		SessionID:  sh.SessionID,
		RepoDir:    sh.RepoDir,
		StartTime:  sh.StartTime,
		EndTime:    sh.EndTime,
		FileSessions: make([]*fileSessionSnapshot, 0, len(sh.FileSessions)),
	}
	for _, fs := range sh.FileSessions {
		fs.mu.Lock()
		fss := &fileSessionSnapshot{
			FilePath:    fs.FilePath,
			TaskRecords: make(map[string][]*taskRecordSnapshot),
		}
		for ttype, records := range fs.TaskRecords {
			for _, rec := range records {
				fss.TaskRecords[string(ttype)] = append(fss.TaskRecords[string(ttype)], &taskRecordSnapshot{
					Type:            string(rec.Type),
					RequestNo:       rec.RequestNo,
					RequestMessages: rec.RequestMessages,
					Response:        rec.Response,
					ToolResults:     rec.ToolResults,
					Duration:        rec.Duration,
					Error:           rec.Error,
				})
			}
		}
		fs.mu.Unlock()
		snap.FileSessions = append(snap.FileSessions, fss)
	}
	sh.mu.Unlock()

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		fmt.Printf("[ocr debug] Failed to marshal session history: %v\n", err)
		return
	}

	debugDir := filepath.Join(sh.RepoDir, "temp")
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		fmt.Printf("[ocr debug] Failed to create debug dir %s: %v\n", debugDir, err)
		return
	}
	filename := filepath.Join(debugDir, fmt.Sprintf("ocr-session-%s.json", sessionName))
	if err := os.WriteFile(filename, data, 0644); err != nil {
		fmt.Printf("[ocr debug] Failed to write session dump to %s: %v\n", filename, err)
	} else {
		fmt.Printf("[ocr debug] Session history written to %s\n", filename)
	}
}

// AppendTaskRecord adds a new task record to the file session for the given
// file path and task type. It auto-assigns the RequestNo based on existing records.
func (fs *FileSession) AppendTaskRecord(taskType TaskType, messages []llm.Message) *TaskRecord {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	rec := &TaskRecord{
		Type:            taskType,
		RequestNo:       len(fs.TaskRecords[taskType]) + 1,
		RequestMessages: copyMessages(messages),
	}
	fs.TaskRecords[taskType] = append(fs.TaskRecords[taskType], rec)
	return rec
}

// copyMessages returns a deep copy of a messages slice so that future mutations
// don't corrupt stored records.
func copyMessages(msgs []llm.Message) []llm.Message {
	cp := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		cp[i] = llm.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			ToolCalls:  append([]llm.ToolCall(nil), m.ToolCalls...),
		}
	}
	return cp
}

// SetResponse records the LLM response in the most recent TaskRecord of the given type.
// It computes estimated token usage locally using tiktoken, independent of any API format.
func (tr *TaskRecord) SetResponse(resp *llm.ChatResponse, duration time.Duration) {
	if resp == nil || len(resp.Choices) == 0 {
		tr.Error = "empty response"
		tr.Duration = duration
		return
	}
	choice := resp.Choices[0]
	content := ""
	if choice.Message.Content != nil {
		content = *choice.Message.Content
	}

	// Estimate tokens using tiktoken — works with any model provider.
	var promptTokens int
	for _, m := range tr.RequestMessages {
		promptTokens += llm.CountTokens(m.ExtractText())
	}
	completionTokens := llm.CountTokens(content)

	usage := &TokenUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	}

	tr.Response = &ResponseRecord{
		Content:   content,
		ToolCalls: choice.Message.ToolCalls,
		Model:     resp.Model,
		Usage:     usage,
	}
	tr.Duration = duration
}

// SetError records an error for this task record.
func (tr *TaskRecord) SetError(err error, duration time.Duration) {
	tr.Error = err.Error()
	tr.Duration = duration
}

// AddToolResult appends a tool call result to this task record.
func (tr *TaskRecord) AddToolResult(toolName, arguments, result string) {
	tr.ToolResults = append(tr.ToolResults, ToolResultRecord{
		ToolName:  toolName,
		Arguments: arguments,
		Result:    result,
	})
}

