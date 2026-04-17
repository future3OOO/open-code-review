package tool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ReviewMode represents the active review mode.
type ReviewMode int

const (
	// ModeWorkspace reads files from the current working tree.
	ModeWorkspace ReviewMode = iota
	// ModeRange reads files as they exist at a specific git ref (--to value).
	ModeRange
	// ModeCommit reads files as they exist at a specific commit hash.
	ModeCommit
)

// ParseReviewMode returns the correct ReviewMode based on provided flag values.
func ParseReviewMode(from, to, commit string) ReviewMode {
	if commit != "" {
		return ModeCommit
	}
	if from != "" && to != "" {
		return ModeRange
	}
	return ModeWorkspace
}

// RefValue returns the git ref that should be used for reading file contents
// in range or commit mode. Returns ("", false) for workspace mode.
func (m ReviewMode) RefValue(toRef, commit string) (string, bool) {
	switch m {
	case ModeRange:
		return toRef, true
	case ModeCommit:
		return commit, true
	default:
		return "", false
	}
}

// FileReader resolves file contents according to the active review mode.
type FileReader struct {
	RepoDir string
	Mode    ReviewMode
	// Ref is the git ref to use for ModeRange (--to) or ModeCommit (--commit).
	// Empty for ModeWorkspace.
	Ref string
}

// Read returns the full content of a file path (relative to RepoDir),
// resolved according to the active review mode.
// - Workspace: reads directly from the filesystem.
// - Range / Commit: uses `git show <Ref>:<path>` to read at the given ref.
func (fr *FileReader) Read(path string) (string, error) {
	switch fr.Mode {
	case ModeWorkspace:
		return fr.readFromDisk(path)
	case ModeRange, ModeCommit:
		return fr.readFromGitShow(path)
	default:
		return fr.readFromDisk(path)
	}
}

func (fr *FileReader) readFromDisk(path string) (string, error) {
	fullPath := filepath.Join(fr.RepoDir, path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", path, err)
	}
	return string(content), nil
}

func (fr *FileReader) readFromGitShow(path string) (string, error) {
	cmd := exec.Command("git", "show", fr.Ref+":"+path)
	cmd.Dir = fr.RepoDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git show %s:%s: %w", fr.Ref, path, err)
	}
	return string(output), nil
}
