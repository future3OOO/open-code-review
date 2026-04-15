package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/argus-review/argus/internal/model"
)

func outputText(comments []model.LlmComment) {
	if len(comments) == 0 {
		fmt.Println("No comments generated.")
		return
	}
	for _, c := range comments {
		fmt.Printf("--- %s:%d-%d ---\n", c.Path, c.StartLine, c.EndLine)
		fmt.Println(c.Content)
		if c.SuggestionCode != "" {
			fmt.Printf("\n```suggestion\n%s\n```\n", c.SuggestionCode)
		}
		fmt.Println()
	}
}

func outputJSON(comments []model.LlmComment) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(comments)
}
