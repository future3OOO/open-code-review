package tool

import (
	"fmt"
	"strings"
)

const fileReadMaxLines = 500

// FileReadProvider reads file content at a given path and optional line range.
type FileReadProvider struct {
	FileReader *FileReader
}

func NewFileRead(fr *FileReader) *FileReadProvider { return &FileReadProvider{FileReader: fr} }

func (p *FileReadProvider) Tool() Tool { return FileRead }

func (p *FileReadProvider) Execute(args map[string]any) (string, error) {
	filePath, _ := args["file_path"].(string)
	if filePath == "" {
		return "Error: file_path is required", nil
	}

	startLine, hasStart := args["start_line"].(float64)
	endLine, hasEnd := args["end_line"].(float64)
	if !hasStart || startLine <= 0 {
		startLine = 1
	}
	if !hasEnd || endLine <= 0 {
		endLine = 0 // means "to end of file"
	}

	content, err := p.FileReader.Read(filePath)
	if err != nil {
		return "", fmt.Errorf("file %q not found: %w", filePath, err)
	}

	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	// Adjust endLine: if not specified or beyond file length, clamp to file length.
	actualEnd := int(endLine)
	if actualEnd <= 0 || actualEnd > totalLines {
		actualEnd = totalLines
	}

	start := int(startLine) - 1
	if start >= totalLines {
		return "", fmt.Errorf("file %q has only %d lines, requested range %d-%d", filePath, totalLines, int(startLine), int(endLine))
	}

	// Apply 500-line cap and track truncation.
	truncated := false
	requestedCount := actualEnd - start
	if requestedCount > fileReadMaxLines {
		actualEnd = start + fileReadMaxLines
		truncated = true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("File: %s (Total lines: %d)\n", filePath, totalLines))
	sb.WriteString(fmt.Sprintf("IS_TRUNCATED: %t\n", truncated))
	sb.WriteString(fmt.Sprintf("LINE_RANGE: %d-%d\n", int(startLine), actualEnd))
	// The following is the original content of the file.
	for i := start; i < actualEnd; i++ {
		sb.WriteString(fmt.Sprintf("%d|%s\n", i+1, lines[i]))
	}
	if truncated {
		sb.WriteString(fmt.Sprintf("\nNote: Results truncated to %d lines. Please narrow your line range.\n", fileReadMaxLines))
	}
	return sb.String(), nil
}
