package tool

import (
	"os"
	"path/filepath"
	"strings"
)

const fileFindMaxCount = 100

var ignoredStartPatterns = []string{
	"test/report/",
	"_packages/",
	"target/",
	".happypack/",
	"report/",
	".cachefile/",
	"iscroll/",
	"app/proxy-class/",
	"mocks_data/",
	"tool_qingxi/",
	".idea/",
	".vscode/",
	"pkgs/",
}

var ignoredIncludePatterns = []string{
	"node_modules/",
	"highcharts",
	"node_modules_bak/",
	".idea",
	".bak",
	"node_modules",
	"webapp/ace/demo/",
	"/.m2/",
	"/assets/font-awesome/",
	"kylin_modules/",
	".pref",
	"/.settings/",
	"/.dep_create/",
	"/.svn/",
	"/font-awesome/",
	"/kitchen-sink/",
	"/_CodeSignature/",
	"vendor/",
}

// FileFindProvider finds files by name or pattern in the repository.
type FileFindProvider struct {
	FileReader *FileReader
}

func NewFileFind(fr *FileReader) *FileFindProvider { return &FileFindProvider{FileReader: fr} }

func (p *FileFindProvider) Tool() Tool { return FileFind }

func (p *FileFindProvider) Execute(args map[string]any) (string, error) {
	queryName, _ := args["query_name"].(string)
	if strings.TrimSpace(queryName) == "" {
		return "// The file was not found", nil
	}

	caseSensitive, _ := args["case_sensitive"].(bool)

	var matched []string
	err := filepath.Walk(p.FileReader.RepoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(p.FileReader.RepoDir, path)
		if isIgnored(relPath) {
			return nil
		}
		base := relPath
		if idx := strings.LastIndex(relPath, string(os.PathSeparator)); idx != -1 {
			base = relPath[idx+1:]
		}
		match := false
		if caseSensitive {
			match = strings.Contains(base, queryName)
		} else {
			match = strings.Contains(strings.ToLower(base), strings.ToLower(queryName))
		}
		if match {
			matched = append(matched, relPath)
		}
		if len(matched) >= fileFindMaxCount {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return "// The file was not found", nil
	}

	if len(matched) == 0 {
		return "// The file was not found", nil
	}
	return strings.Join(matched, "\n"), nil
}

func isIgnored(path string) bool {
	// Exclude files without extension
	if !strings.Contains(path, ".") {
		return true
	}
	for _, p := range ignoredStartPatterns {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	for _, p := range ignoredIncludePatterns {
		if strings.Contains(path, p) {
			return true
		}
	}
	return false
}
