package main

import (
	"fmt"
	"time"

	"github.com/argus-review/argus/internal/config/testconnection"
	"github.com/argus-review/argus/internal/llm"
)

func runLLM(args []string) error {
	if len(args) == 0 {
		printLLMUsage()
		return nil
	}

	switch args[0] {
	case "test":
		return runLLMTest()
	default:
		return fmt.Errorf("unknown llm sub-command: %s\nRun 'argus llm' for usage", args[0])
	}
}

func runLLMTest() error {
	cfg, err := LoadAppConfig(defaultConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg == nil || cfg.Llm.URL == "" || cfg.Llm.AuthToken == "" {
		return fmt.Errorf("llm.url and llm.auth_token are required in %s", defaultConfigPath())
	}

	task, err := testconnection.LoadDefault()
	if err != nil {
		return fmt.Errorf("load test task config: %w", err)
	}

	timeout := 30 * time.Second
	if task.Timeout > 0 {
		timeout = time.Duration(task.Timeout) * time.Second
	}

	llmClient := llm.NewClient(llm.ClientConfig{
		URL:     cfg.Llm.URL,
		APIKey:  cfg.Llm.AuthToken,
		Model:   cfg.Llm.Model,
		Timeout: timeout,
	})

	messages := make([]llm.Message, 0, len(task.Messages))
	for _, m := range task.Messages {
		messages = append(messages, llm.Message{Role: m.Role, Content: m.Content})
	}

	resp, err := llmClient.GeneralRequest(messages, cfg.Llm.Model, nil)
	if err != nil {
		return fmt.Errorf("llm request failed: %w", err)
	}

	model := cfg.Llm.Model
	if resp.Model != "" {
		model = resp.Model
	}
	fmt.Printf("Model: %s\n", model)
	fmt.Printf("%s\n", resp.Content())
	return nil
}

func printLLMUsage() {
	fmt.Println(`LLM utility commands.

Usage:
  argus llm <sub-command>

Sub-commands:
  test         Send a test conversation to the configured LLM model

Examples:
  argus llm test                 Verify LLM connectivity and configuration`)
}
