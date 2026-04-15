package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

type cliOptions struct {
	configPath       string
	repoDir          string
	baseRef          string
	headRef          string
	staged           bool
	llmBaseURL       string
	llmAPIKey        string
	llmModel         string
	llmTimeout       time.Duration
	concurrency      int
	perFileTimeout   int
	dryRun           bool
	outputFormat     string
	showHelp         bool
}

func parseFlags() (cliOptions, error) {
	// Quick check for -h before full parsing
	for _, arg := range os.Args[1:] {
		if arg == "-h" || arg == "--help" {
			return cliOptions{showHelp: true}, nil
		}
	}

	fs := flag.NewFlagSet("argus", flag.ContinueOnError)

	opts := cliOptions{}

	fs.StringVar(&opts.configPath, "config", "argus.yaml", "path to YAML config file")
	fs.StringVar(&opts.repoDir, "repo", "", "root directory of the git repository (default: current dir)")
	fs.StringVar(&opts.baseRef, "base", "", "base ref for diff range (e.g., 'main')")
	fs.StringVar(&opts.headRef, "head", "", "head ref for diff range (e.g., 'feature-branch')")
	fs.BoolVar(&opts.staged, "staged", false, "review staged changes instead of base..head")
	fs.StringVar(&opts.outputFormat, "format", "text", "output format: text or json")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "run review without submitting comments (testing mode)")
	fs.IntVar(&opts.concurrency, "concurrency", 4, "max concurrent file reviews")
	fs.IntVar(&opts.perFileTimeout, "timeout", 10, "per-file timeout in minutes")

	// LLM connection flags
	fs.StringVar(&opts.llmBaseURL, "llm-url", os.Getenv("ARGUS_LLM_BASE_URL"), "LLM service base URL (OPENAI_COMPATIBLE)")
	fs.StringVar(&opts.llmAPIKey, "llm-api-key", os.Getenv("ARGUS_LLM_API_KEY"), "LLM API key")
	fs.StringVar(&opts.llmModel, "llm-model", os.Getenv("ARGUS_LLM_MODEL"), "LLM model name (overrides config template default)")
	fs.DurationVar(&opts.llmTimeout, "llm-timeout", 5*time.Minute, "LLM request timeout")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return opts, fmt.Errorf("parse flags: %w - use -h for help", err)
	}

	if opts.llmBaseURL == "" {
		return opts, fmt.Errorf("--llm-url is required or set ARGUS_LLM_BASE_URL env var")
	}
	if opts.llmAPIKey == "" {
		return opts, fmt.Errorf("--llm-api-key is required or set ARGUS_LLM_API_KEY env var")
	}

	if !opts.staged && opts.baseRef == "" && opts.headRef == "" {
		return opts, fmt.Errorf("either --staged or both --base and --head refs are required")
	}

	return opts, nil
}

func printUsage() {
	fmt.Println(`Argus - AI-powered Code Review CLI

Usage: argus [flags]

Examples:
  # Review staged changes
  argus --staged --config argus.yaml

  # Review a specific range
  argus --base main --head feature-branch --config argus.yaml

Flags:`)

	// Re-create the same FlagSet to print defaults
	fs := flag.NewFlagSet("argus", flag.ContinueOnError)
	var opts cliOptions
	fs.StringVar(&opts.configPath, "config", "argus.yaml", "path to YAML config file")
	fs.StringVar(&opts.repoDir, "repo", "", "root directory of the git repository (default: current dir)")
	fs.StringVar(&opts.baseRef, "base", "", "base ref for diff range (e.g., 'main')")
	fs.StringVar(&opts.headRef, "head", "", "head ref for diff range (e.g., 'feature-branch')")
	fs.BoolVar(&opts.staged, "staged", false, "review staged changes instead of base..head")
	fs.StringVar(&opts.outputFormat, "format", "text", "output format: text or json")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "run review without submitting comments (testing mode)")
	fs.IntVar(&opts.concurrency, "concurrency", 4, "max concurrent file reviews")
	fs.IntVar(&opts.perFileTimeout, "timeout", 10, "per-file timeout in minutes")
	fs.StringVar(&opts.llmBaseURL, "llm-url", "", "LLM service base URL (OPENAI_COMPATIBLE)")
	fs.StringVar(&opts.llmAPIKey, "llm-api-key", "", "LLM API key")
	fs.StringVar(&opts.llmModel, "llm-model", "", "LLM model name (overrides config template default)")
	fs.DurationVar(&opts.llmTimeout, "llm-timeout", 5*time.Minute, "LLM request timeout")
	fs.PrintDefaults()
	fmt.Println(`
Environment Variables:
  ARGUS_LLM_BASE_URL    LLM service base URL (OpenAI-compatible endpoint)
  ARGUS_LLM_API_KEY     LLM API bearer token
  ARGUS_LLM_MODEL       Default model name`)
}
