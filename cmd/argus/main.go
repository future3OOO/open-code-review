// Argus is an AI-powered code review CLI tool.
// It reads git diffs, sends them to a configurable LLM service, and generates review comments.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/argus-review/argus/internal/agent"
	"github.com/argus-review/argus/internal/config"
	"github.com/argus-review/argus/internal/llm"
	"github.com/argus-review/argus/internal/tool"
	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse CLI flags
	opts, err := parseFlags()
	if err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if opts.showHelp {
		printUsage()
		return nil
	}

	// Load config template from YAML
	tpl, err := loadTemplate(opts.configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := tpl.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Resolve repository directory
	repoDir, err := resolveRepoDir(opts.repoDir)
	if err != nil {
		return fmt.Errorf("resolve repo: %w", err)
	}

	// Build LLM client
	llmClient := llm.NewClient(llm.ClientConfig{
		BaseURL: opts.llmBaseURL,
		APIKey:  opts.llmAPIKey,
		Model:   opts.llmModel,
		Timeout: opts.llmTimeout,
	})

	// Build tool registry — users register their own tool implementations here
	tools := buildToolRegistry(opts)

	// Create and run the agent
	ag := agent.New(agent.Args{
		RepoDir:               repoDir,
		BaseRef:               opts.baseRef,
		HeadRef:               opts.headRef,
		UseStaged:             opts.staged,
		Template:              *tpl,
		LLMClient:             llmClient,
		Tools:                 tools,
		MaxConcurrency:        opts.concurrency,
		PerFileTimeoutMinutes: opts.perFileTimeout,
		DryRun:                opts.dryRun,
	})

	ctx := context.Background()
	comments, err := ag.Run(ctx)
	if err != nil {
		return fmt.Errorf("review failed: %w", err)
	}

	// Output results
	if opts.outputFormat == "json" {
		return outputJSON(comments)
	}
	outputText(comments)

	return nil
}

// loadTemplate reads and unmarshals the YAML configuration file.
func loadTemplate(path string) (*config.Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}
	var tpl config.Template
	if err := yaml.Unmarshal(data, &tpl); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &tpl, nil
}

// resolveRepoDir returns the absolute path to the repository directory.
func resolveRepoDir(input string) (string, error) {
	if input == "" {
		var err error
		input, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
	}
	absPath, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	// Verify it's a git repo
	out, err := runGitCmd(absPath, "rev-parse", "--git-dir")
	if err != nil || len(out) == 0 {
		return "", fmt.Errorf("%s is not a git repository", absPath)
	}
	return absPath, nil
}

// buildToolRegistry creates a registry with available tools.
// TODO: Wire up real tool implementations here.
// For now, this is a placeholder — users should register their own providers.
func buildToolRegistry(_ cliOptions) tool.Registry {
	reg := tool.NewRegistry()
	// Register built-in or stub tools. Users should replace these with real implementations.
	for _, t := range []tool.Tool{
		tool.FileRead, tool.FileFind, tool.FileReadDiff, tool.FileSearch, tool.CodeSearch,
	} {
		reg.Register(tool.NewStub(t))
	}
	return reg
}
