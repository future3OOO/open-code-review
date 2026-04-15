package config

// Example tool implementation stubs.
// These are left as blanks — users should implement their own versions or use the CLI default stubs.
// Each Provider implements tool.Provider interface from internal/tool.

import (
	"github.com/argus-review/argus/internal/tool"
)

// ===== PLACEHOLDER TOOL IMPLEMENTATIONS =====
// Below are skeleton implementations showing how to register real tools.
// Replace the Execute method bodies with actual logic for each tool.

// FileReadProvider reads file content at a given path and optional line range.
type FileReadProvider struct{}

func (p *FileReadProvider) Tool() tool.Tool { return tool.FileRead }

func (p *FileReadProvider) Execute(args map[string]any) (string, error) {
	// TODO: Implement file reading logic.
	// args["path"]       -> file path
	// args["start_line"] -> optional start line (1-indexed)
	// args["end_line"]   -> optional end line (1-indexed)
	return "Not implemented: Register your own FileReadProvider", nil
}

// FileFindProvider finds files by name or pattern in the repository.
type FileFindProvider struct{}

func (p *FileFindProvider) Tool() tool.Tool { return tool.FileFind }

func (p *FileFindProvider) Execute(args map[string]any) (string, error) {
	// TODO: Implement file finding logic.
	// args["pattern"] -> glob pattern (e.g., "*.go")
	return "Not implemented: Register your own FileFindProvider", nil
}

// FileReadDiffProvider reads diff content between two refs.
type FileReadDiffProvider struct{}

func (p *FileReadDiffProvider) Tool() tool.Tool { return tool.FileReadDiff }

func (p *FileReadDiffProvider) Execute(args map[string]any) (string, error) {
	// TODO: Implement git diff logic.
	// args["base_ref"] -> base reference
	// args["head_ref"] -> head reference
	// args["file_path"]-> optional specific file
	return "Not implemented: Register your own FileReadDiffProvider", nil
}

// FileSearchProvider searches for text patterns within files.
type FileSearchProvider struct{}

func (p *FileSearchProvider) Tool() tool.Tool { return tool.FileSearch }

func (p *FileSearchProvider) Execute(args map[string]any) (string, error) {
	// TODO: Implement text search in files (e.g., grep-like).
	// args["query"]      -> search pattern
	// args["file_pattern"]-> optional file filter (e.g., "*.go")
	return "Not implemented: Register your own FileSearchProvider", nil
}

// CodeSearchProvider performs semantic/code-aware code search.
type CodeSearchProvider struct{}

func (p *CodeSearchProvider) Tool() tool.Tool { return tool.CodeSearch }

func (p *CodeSearchProvider) Execute(args map[string]any) (string, error) {
	// TODO: Implement semantic code search (e.g., via code indexer or external API).
	// args["query"]        -> search query
	// args["symbol_name"]  -> symbol to find references of
	return "Not implemented: Register your own CodeSearchProvider", nil
}

// CodeCommentProvider submits review comments to the platform (PR/MR system).
// This is the most integration-specific tool — it depends on where you host your code.
type CodeCommentProvider struct{}

func (p *CodeCommentProvider) Tool() tool.Tool { return tool.CodeComment }

func (p *CodeCommentProvider) Execute(args map[string]any) (string, error) {
	// TODO: Implement comment submission to your PR/MR system.
	// args["comments"] -> array of {content, existing_code, suggestion_code, file, line}
	// Returns success/failure message for the LLM loop to continue.
	return tool.CommentSucceed, nil
}
