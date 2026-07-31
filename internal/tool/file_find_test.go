package tool

import (
	"context"
	"testing"
)

func TestFileFindExecuteMatchesPathComponentsAtRef(t *testing.T) {
	dir := setupTestRepo(t)
	provider := NewFileFind(&FileReader{RepoDir: dir, Ref: getHeadCommit(t, dir)})

	for _, query := range []string{"pkg", "util.go"} {
		result, err := provider.Execute(context.Background(), map[string]any{"query_name": query})
		if err != nil {
			t.Fatal(err)
		}
		if result != "pkg/util.go" {
			t.Fatalf("query %q: expected pkg/util.go, got %q", query, result)
		}
	}
}
