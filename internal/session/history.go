// Package session provides a session history mechanism for collecting conversation
// records during code review task execution. It organizes records by file path
// and request type (plan_task, main_task, memory_compression_task).
package session

import (
	"sync"
	"time"

	"github.com/argus-review/argus/internal/llm"
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

// ResponseRecord holds the parsed LLM response.
type ResponseRecord struct {
	Content   string
	ToolCalls []llm.ToolCall
	Model     string
	Usage     *llm.Usage
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
	defer sh.mu.Unlock()
	sh.EndTime = time.Now()
}

// AppendTaskRecord adds a new task record to the file session for the given
// file path and task type. It auto-assigns the RequestNo based on existing records.
func (fs *FileSession) AppendTaskRecord(taskType TaskType, messages []llm.Message) *TaskRecord {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	rec := &TaskRecord{
		Type:            taskType,
		RequestNo:       len(fs.TaskRecords[taskType]) + 1,
		RequestMessages: messages,
	}
	fs.TaskRecords[taskType] = append(fs.TaskRecords[taskType], rec)
	return rec
}

// SetResponse records the LLM response in the most recent TaskRecord of the given type.
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
	tr.Response = &ResponseRecord{
		Content:   content,
		ToolCalls: choice.Message.ToolCalls,
		Model:     resp.Model,
		Usage:     resp.Usage,
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

