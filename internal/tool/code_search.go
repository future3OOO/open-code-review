package tool

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const gitGrepMaxCount = 100

// CodeSearchProvider performs text search across the repository using git grep.
type CodeSearchProvider struct {
	FileReader *FileReader
}

func NewCodeSearch(fr *FileReader) *CodeSearchProvider { return &CodeSearchProvider{FileReader: fr} }

func (p *CodeSearchProvider) Tool() Tool { return CodeSearch }

func (p *CodeSearchProvider) Execute(args map[string]any) (string, error) {
	searchText, _ := args["search_text"].(string)
	caseSensitive, _ := args["case_sensitive"].(bool)
	usePerlRegexp, _ := args["use_perl_regexp"].(bool)

	filePatternsIface, _ := args["file_patterns"].([]any)
	var patterns []string
	for _, item := range filePatternsIface {
		if s, ok := item.(string); ok && s != "" {
			patterns = append(patterns, s)
		}
	}

	if strings.TrimSpace(searchText) == "" {
		return "Error: search_text is blank", nil
	}

	result, err := p.gitGrep(searchText, caseSensitive, usePerlRegexp, patterns)
	if err != nil {
		return "The system encountered some problems when calling the code_search tool. Please try a different tool.", nil
	}
	return result, nil
}

func (p *CodeSearchProvider) gitGrep(searchText string, caseSensitive bool, usePerlRegexp bool, pathspec []string) (string, error) {
	cmdArgs := []string{"--no-pager", "grep"}

	if !caseSensitive {
		cmdArgs = append(cmdArgs, "-i")
	}
	if usePerlRegexp {
		cmdArgs = append(cmdArgs, "-P")
	} else {
		cmdArgs = append(cmdArgs, "-E")
	}

	cmdArgs = append(cmdArgs, "-n", "--no-color")
	cmdArgs = append(cmdArgs, "--max-count", fmt.Sprintf("%d", gitGrepMaxCount))

	// In range/commit mode, specify the ref so git grep searches at that ref instead of working tree.
	if ref := p.FileReader.Ref; ref != "" {
		cmdArgs = append(cmdArgs, ref)
	}

	cmdArgs = append(cmdArgs, "--", searchText)
	cmdArgs = append(cmdArgs, pathspec...)

	cmd := exec.Command("git", cmdArgs...)
	cmd.Dir = p.FileReader.RepoDir

	output, err := cmd.CombinedOutput()
	outStr := string(output)

	if err != nil && outStr == "" {
		return "No matches found", nil
	}

	lines := strings.Split(strings.TrimRight(outStr, "\n"), "\n")
	truncated := len(lines) >= gitGrepMaxCount

	type match struct {
		lineNum int
		content string
	}
	fileMatches := make(map[string][]match)
	var fileOrder []string
	seen := make(map[string]bool)

	var sb strings.Builder
	if truncated {
		sb.WriteString(fmt.Sprintf("Note: The results have been truncated. Only showing first %d results.\n", gitGrepMaxCount))
	}

	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		fname := parts[0]
		m := match{}
		ln, parseErr := strconv.Atoi(parts[1])
		if parseErr != nil {
			continue
		}
		m.lineNum = ln
		m.content = parts[2]
		if !seen[fname] {
			seen[fname] = true
			fileOrder = append(fileOrder, fname)
		}
		fileMatches[fname] = append(fileMatches[fname], m)
	}

	for _, path := range fileOrder {
		matches := fileMatches[path]
		sb.WriteString(fmt.Sprintf("File: %s\nMatch lines: %d\n", path, len(matches)))
		for _, m := range matches {
			sb.WriteString(fmt.Sprintf("%d|%s\n", m.lineNum, m.content))
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}
