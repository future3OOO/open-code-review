package tool

import "strings"

// FileReadDiffProvider retrieves diff content by file path from an already-parsed diff set.
// Translated from Java FileReadDiffTool — uses the existing diff parser instead of
// Repositories.getDiffs().
type FileReadDiffProvider struct {
	DiffMap map[string]string // path -> diff text, same as Agent's internal diffMap
}

func NewFileReadDiff() *FileReadDiffProvider { return &FileReadDiffProvider{DiffMap: make(map[string]string)} }

func (p *FileReadDiffProvider) Tool() Tool { return FileReadDiff }

func (p *FileReadDiffProvider) Execute(args map[string]any) (string, error) {
	pathArray, _ := args["path_array"].([]any)
	if len(pathArray) == 0 {
		return "Error: no files found", nil
	}

	var sb strings.Builder
	for _, item := range pathArray {
		path, ok := item.(string)
		if !ok {
			continue
		}
		if d, exists := p.DiffMap[path]; exists {
			sb.WriteString("==== FILE: ")
			sb.WriteString(path)
			sb.WriteString(" ====\n")
			sb.WriteString(d)
			sb.WriteString("\n")
		}
	}

	result := sb.String()
	if result == "" {
		return "Error: diff not found for the requested paths", nil
	}
	return result, nil
}
