package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/argus-review/argus/internal/agent"
	"github.com/argus-review/argus/internal/config/rules"
	"github.com/argus-review/argus/internal/config/template"
	"github.com/argus-review/argus/internal/config/toolsconfig"
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

	tpl, err := template.LoadDefault()
	if err != nil {
		return fmt.Errorf("load default template: %w", err)
	}
	if err := tpl.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	sysRule, err := rules.LoadDefault()
	if err != nil {
		return fmt.Errorf("load default system rule: %w", err)
	}
	if opts.rulePath != "" {
		sysRule, err = loadSystemRule(opts.rulePath)
		if err != nil {
			return fmt.Errorf("load system rule: %w", err)
		}
	}

	toolEntries, err := toolsconfig.Load(opts.toolConfigPath)
	if err != nil {
		return fmt.Errorf("load tools: %w", err)
	}
	planToolDefs := agent.BuildToolDefs(toolEntries, true)
	mainToolDefs := agent.BuildToolDefs(toolEntries, false)

	repoDir, err := resolveRepoDir(opts.repoDir)
	if err != nil {
		return fmt.Errorf("resolve repo: %w", err)
	}

	cfg, err := LoadAppConfig(defaultConfigPath())
	if err != nil {
		return fmt.Errorf("load app config: %w", err)
	}
	if cfg == nil || cfg.Llm.URL == "" || cfg.Llm.AuthToken == "" {
		return fmt.Errorf("llm.url and llm.auth_token are required in $HOME/.argus/config.json")
	}
	model := cfg.Llm.Model
	llmClient := llm.NewClient(llm.ClientConfig{
		URL:     cfg.Llm.URL,
		APIKey:  cfg.Llm.AuthToken,
		Model:   model,
	})

	collector := tool.NewCommentCollector()
	mode := tool.ParseReviewMode(opts.from, opts.to, opts.commit)
	ref, _ := mode.RefValue(opts.to, opts.commit)
	diffMap := make(map[string]string)
	fileReader := &tool.FileReader{
		RepoDir: repoDir,
		Mode:    mode,
		Ref:     ref,
	}
	tools := buildToolRegistry(collector, fileReader, diffMap)

	ag := agent.New(agent.Args{
		RepoDir:               repoDir,
		From:                  opts.from,
		To:                    opts.to,
		Commit:                opts.commit,
		DiffMap:               diffMap,
		Template:              *tpl,
		SystemRule:            sysRule,
		LLMClient:             llmClient,
		Tools:                 tools,
		PlanToolDefs:          planToolDefs,
		MainToolDefs:          mainToolDefs,
		CommentCollector:      collector,
		MaxConcurrency:        opts.concurrency,
		PerFileTimeoutMinutes: opts.perFileTimeout,
		Model:                 model,
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

func loadSystemRule(path string) (*rules.SystemRule, error) {
	return rules.LoadFile(path)
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

func buildToolRegistry(collector *tool.CommentCollector, fr *tool.FileReader, diffMap map[string]string) tool.Registry {
	reg := tool.NewRegistry()
	reg.Register(tool.NewFileRead(fr))
	reg.Register(tool.NewFileFind(fr))
	reg.Register(tool.NewFileReadDiff())
	reg.Register(tool.NewFileSearch(fr))
	reg.Register(tool.NewCodeSearch(fr))
	reg.Register(&tool.CodeCommentProvider{Collector: collector})

	// Wire up FileReadDiffProvider with shared diffMap pointer so Agent's loadDiffs populates it.
	if p, ok := reg[tool.FileReadDiff.Name()].(*tool.FileReadDiffProvider); ok {
		p.DiffMap = diffMap
	}

	return reg
}
