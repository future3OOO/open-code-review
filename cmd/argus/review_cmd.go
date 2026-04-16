package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/argus-review/argus/internal/agent"
	"github.com/argus-review/argus/internal/config"
	"github.com/argus-review/argus/internal/llm"
	"github.com/argus-review/argus/internal/tool"
)

func runReview(args []string) error {
	opts, err := parseReviewFlags(args)
	if err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if opts.showHelp {
		printReviewUsage()
		return nil
	}

	tpl, err := loadTemplate(opts.configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := tpl.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	var sysRule *config.SystemRule
	if opts.rulePath != "" {
		sysRule, err = loadSystemRule(opts.rulePath)
		if err != nil {
			return fmt.Errorf("load system rule: %w", err)
		}
	}

	toolEntries, err := config.LoadTools(opts.toolConfigPath)
	if err != nil {
		return fmt.Errorf("load tools: %w", err)
	}
	planToolDefs := agent.BuildToolDefs(toolEntries, true)
	mainToolDefs := agent.BuildToolDefs(toolEntries, false)

	repoDir, err := resolveRepoDir(opts.repoDir)
	if err != nil {
		return fmt.Errorf("resolve repo: %w", err)
	}

	llmClient := llm.NewClient(llm.ClientConfig{
		BaseURL: opts.llmBaseURL,
		APIKey:  opts.llmAPIKey,
		Model:   opts.llmModel,
		Timeout: opts.llmTimeout,
	})

	collector := tool.NewCommentCollector()
	tools := buildToolRegistry(collector)

	ag := agent.New(agent.Args{
		RepoDir:               repoDir,
		From:                  opts.from,
		To:                    opts.to,
		Commit:                opts.commit,
		Template:              *tpl,
		SystemRule:            sysRule,
		LLMClient:             llmClient,
		Tools:                 tools,
		PlanToolDefs:          planToolDefs,
		MainToolDefs:          mainToolDefs,
		CommentCollector:      collector,
		MaxConcurrency:        opts.concurrency,
		PerFileTimeoutMinutes: opts.perFileTimeout,
		DryRun:                opts.dryRun,
	})

	ctx := context.Background()
	comments, err := ag.Run(ctx)
	if err != nil {
		return fmt.Errorf("review failed: %w", err)
	}

	if opts.outputFormat == "json" {
		return outputJSON(comments)
	}
	outputText(comments)

	return nil
}

// These helpers are shared between subcommands.

func loadTemplate(path string) (*config.Template, error) {
	return config.LoadTemplate(path)
}

func loadSystemRule(path string) (*config.SystemRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rule file %s: %w", path, err)
	}
	var rule config.SystemRule
	if err := json.Unmarshal(data, &rule); err != nil {
		return nil, fmt.Errorf("unmarshal rule file: %w", err)
	}
	return &rule, nil
}

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
	out, err := runGitCmd(absPath, "rev-parse", "--git-dir")
	if err != nil || len(out) == 0 {
		return "", fmt.Errorf("%s is not a git repository", absPath)
	}
	return absPath, nil
}

func buildToolRegistry(collector *tool.CommentCollector) tool.Registry {
	reg := tool.NewRegistry()
	for _, t := range []tool.Tool{
		tool.FileRead, tool.FileFind, tool.FileReadDiff, tool.FileSearch, tool.CodeSearch,
	} {
		reg.Register(tool.NewStub(t))
	}
	reg.Register(&tool.CodeCommentProvider{Collector: collector})
	return reg
}
