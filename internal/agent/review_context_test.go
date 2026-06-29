package agent

import (
	"strings"
	"testing"
)

func TestRequirementBackgroundInjectsOnlyCurrentFileReviewContext(t *testing.T) {
	agent := New(Args{
		Background: "existing background",
		ReviewContext: map[string]string{
			"src/app.go":   "prior app context",
			"src/other.go": "prior other context",
		},
	})

	background := agent.requirementBackground("src/app.go")

	if !strings.Contains(background, "existing background") {
		t.Fatalf("existing background missing:\n%s", background)
	}
	if !strings.Contains(background, "prior app context") {
		t.Fatalf("current file context missing:\n%s", background)
	}
	if strings.Contains(background, "prior other context") {
		t.Fatalf("other file context leaked:\n%s", background)
	}
}

func TestRequirementBackgroundNoContextMatchesExistingBackground(t *testing.T) {
	withoutContext := New(Args{Background: "existing background"}).
		requirementBackground("src/app.go")
	withUnmatchedContext := New(Args{
		Background: "existing background",
		ReviewContext: map[string]string{
			"src/other.go": "prior other context",
		},
	}).requirementBackground("src/app.go")

	if withoutContext != withUnmatchedContext {
		t.Fatalf("no-context background changed\nwithout:\n%s\nwith unmatched:\n%s", withoutContext, withUnmatchedContext)
	}
	if strings.Contains(withoutContext, "Prior unresolved review thread context") {
		t.Fatalf("no-context background contains context header:\n%s", withoutContext)
	}
}
