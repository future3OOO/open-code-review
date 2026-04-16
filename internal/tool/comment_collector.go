package tool

import (
	"sync"

	"github.com/argus-review/argus/internal/model"
)

// CommentCollector is a thread-safe, per-Agent comment store.
// Each Agent instance owns its own collector so reviews across different repos do not interfere.
type CommentCollector struct {
	mu       sync.Mutex
	comments []model.LlmComment
}

// NewCommentCollector creates an empty collector.
func NewCommentCollector() *CommentCollector {
	return &CommentCollector{}
}

// Add appends a comment to the collector.
func (c *CommentCollector) Add(cm model.LlmComment) {
	c.mu.Lock()
	c.comments = append(c.comments, cm)
	c.mu.Unlock()
}

// Comments returns a copy of all collected comments.
func (c *CommentCollector) Comments() []model.LlmComment {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]model.LlmComment, len(c.comments))
	copy(out, c.comments)
	return out
}
