package tool

import (
	"fmt"
	"strings"
)

// FileSearchProvider searches for text patterns within a file (grep-like).
type FileSearchProvider struct {
	FileReader *FileReader
}

func NewFileSearch(fr *FileReader) *FileSearchProvider { return &FileSearchProvider{FileReader: fr} }

func (p *FileSearchProvider) Tool() Tool { return FileSearch }

func (p *FileSearchProvider) Execute(args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	searchText, _ := args["search_text"].(string)
	caseSensitive, _ := args["case_sensitive"].(bool)

	if strings.TrimSpace(searchText) == "" {
		return "Error: search_text is blank", nil
	}
	if strings.TrimSpace(path) == "" {
		return "Error: path is blank", nil
	}

	content, err := p.FileReader.Read(path)
	if err != nil {
		return fmt.Sprintf("Error: file %q not found: %v", path, err), nil
	}

	if strings.TrimSpace(content) == "" {
		return "Error: file content is empty", nil
	}

	lines := strings.Split(content, "\n")
	var sb strings.Builder
	matchCount := 0

	for i, line := range lines {
		lineNum := i + 1
		targetLine := line
		targetSearch := searchText
		if !caseSensitive {
			targetLine = strings.ToLower(line)
			targetSearch = strings.ToLower(searchText)
		}
		if strings.Contains(targetLine, targetSearch) {
			sb.WriteString(fmt.Sprintf("%d|%s\n", lineNum, line))
			matchCount++
		}
	}

	if matchCount == 0 {
		return "No matches found", nil
	}

	result := fmt.Sprintf("File: %s\nMatch lines: %d\n%s", path, matchCount, sb.String())
	return result, nil
}
