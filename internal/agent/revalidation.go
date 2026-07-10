package agent

import (
	"context"
	"sort"

	"github.com/open-code-review/open-code-review/internal/diff"
	"github.com/open-code-review/open-code-review/internal/model"
	reviewtool "github.com/open-code-review/open-code-review/internal/tool"
)

func (a *Agent) revalidateUnchangedPaths(ctx context.Context) {
	grouped := make(map[string][]model.LlmComment)
	for _, finding := range a.args.Revalidate {
		if _, changed := a.changedPaths[finding.Path]; !changed {
			grouped[finding.Path] = append(grouped[finding.Path], finding)
		}
	}
	paths := make([]string, 0, len(grouped))
	for path := range grouped {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	provider, ok := lookupTool(a.args.Tools, reviewtool.FileRead).(*reviewtool.FileReadProvider)
	if !ok || provider.FileReader == nil {
		for _, path := range paths {
			a.recordWarning("revalidation_incomplete", path, "current-file reader is unavailable")
		}
		return
	}
	for _, path := range paths {
		content, err := provider.FileReader.Read(ctx, path)
		if err != nil {
			a.recordWarning("revalidation_incomplete", path, "open finding source could not be read at the target ref")
			continue
		}
		current := model.Diff{OldPath: path, NewPath: path, NewFileContent: content}
		for _, finding := range grouped[path] {
			finding.StartLine = 0
			finding.EndLine = 0
			if !diff.ResolveComment(&finding, &current) {
				a.recordWarning("revalidation_incomplete", path, "open finding anchor could not be resolved at the target ref")
				continue
			}
			a.args.CommentCollector.Add(finding)
		}
		a.executeReviewFilter(ctx, current, path, 0)
	}
}

func (a *Agent) seedRevalidationFindings(d model.Diff) {
	for _, finding := range a.args.Revalidate {
		if finding.Path != d.NewPath && finding.Path != d.OldPath {
			continue
		}
		finding.Path = d.NewPath
		finding.StartLine = 0
		finding.EndLine = 0
		if !diff.ResolveComment(&finding, &d) {
			a.recordWarning("revalidation_incomplete", finding.Path, "open finding anchor could not be resolved in this rerun delta")
			continue
		}
		a.args.CommentCollector.Add(finding)
	}
}
