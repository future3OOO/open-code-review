package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

// --- custom flag set that supports short flags (-c, -f etc.) ---

type argusFlagSet struct {
	fs       *flag.FlagSet
	shortMap map[string]string // maps short key "c" -> full name "commit"
	showHelp bool
}

func newArgusFlagSet(name string) *argusFlagSet {
	return &argusFlagSet{
		fs:       flag.NewFlagSet(name, flag.ContinueOnError),
		shortMap: make(map[string]string),
	}
}

// StringVarP registers --name with optional short form -s.
func (a *argusFlagSet) StringVarP(p *string, name, shorthand string, value, usage string) {
	suffix := ""
	if shorthand != "" {
		a.shortMap[shorthand] = name
		suffix = fmt.Sprintf(" (shorthand: -%s)", shorthand)
	}
	a.fs.StringVar(p, name, value, usage+suffix)
}

// BoolVarP registers --name with optional short form -s.
func (a *argusFlagSet) BoolVarP(p *bool, name, shorthand string, value bool, usage string) {
	suffix := ""
	if shorthand != "" {
		a.shortMap[shorthand] = name
		suffix = fmt.Sprintf(" (shorthand: -%s)", shorthand)
	}
	a.fs.BoolVar(p, name, value, usage+suffix)
}

func (a *argusFlagSet) StringVar(p *string, name string, value string, usage string) {
	a.fs.StringVar(p, name, value, usage)
}

func (a *argusFlagSet) BoolVar(p *bool, name string, value bool, usage string) {
	a.fs.BoolVar(p, name, value, usage)
}

func (a *argusFlagSet) IntVar(p *int, name string, value int, usage string) {
	a.fs.IntVar(p, name, value, usage)
}

func (a *argusFlagSet) DurationVar(p *time.Duration, name string, value time.Duration, usage string) {
	a.fs.DurationVar(p, name, value, usage)
}

func (a *argusFlagSet) PrintDefaults() {
	a.fs.PrintDefaults()
}

func (a *argusFlagSet) Parse(arguments []string) error {
	expanded := expandShortFlags(arguments, a.shortMap)

	for _, arg := range expanded {
		if arg == "-h" || arg == "--help" {
			a.showHelp = true
			return nil
		}
	}

	return a.fs.Parse(expanded)
}

// expandShortFlags replaces standalone -X args with their long equivalents.
// Only triggers when the arg is exactly -N (single char after dash).
func expandShortFlags(args []string, shortMap map[string]string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if len(arg) == 2 && arg[0] == '-' && arg[1] != '-' {
			key := string(arg[1])
			if full, ok := shortMap[key]; ok {
				out = append(out, "--"+full)
				continue
			}
		}
		out = append(out, arg)
	}
	return out
}

// --- review subcommand options ---

type reviewOptions struct {
	configPath     string
	toolConfigPath string
	rulePath       string
	repoDir        string
	from           string
	to             string
	commit         string
	outputFormat   string
	dryRun         bool
	concurrency    int
	perFileTimeout int
	llmBaseURL     string
	llmAPIKey      string
	llmModel       string
	llmTimeout     time.Duration
	showHelp       bool
}

func parseReviewFlags(args []string) (reviewOptions, error) {
	a := newArgusFlagSet("argus review")

	opts := reviewOptions{}

	a.StringVar(&opts.configPath, "config", "argus.yaml", "path to YAML config file")
	a.StringVar(&opts.toolConfigPath, "tools", "", "path to JSON tools config file (default: argus-tools.json)")
	a.StringVar(&opts.rulePath, "rule", "", "path to JSON file with system review rules")
	a.StringVar(&opts.repoDir, "repo", "", "root directory of the git repository (default: current dir)")
	a.StringVar(&opts.from, "from", "", "source ref to start diff from (e.g., 'main')")
	a.StringVar(&opts.to, "to", "", "target ref to end diff at (e.g., 'feature-branch')")
	a.StringVarP(&opts.commit, "commit", "c", "", "single commit hash or tag to review (vs its parent)")
	a.StringVarP(&opts.outputFormat, "format", "f", "text", "output format: text or json")
	a.BoolVar(&opts.dryRun, "dry-run", false, "run review without submitting comments (testing mode)")
	a.IntVar(&opts.concurrency, "concurrency", 4, "max concurrent file reviews")
	a.IntVar(&opts.perFileTimeout, "timeout", 10, "per-file timeout in minutes")
	a.StringVar(&opts.llmBaseURL, "llm-url", os.Getenv("ARGUS_LLM_BASE_URL"), "LLM service base URL (OPENAI_COMPATIBLE)")
	a.StringVar(&opts.llmAPIKey, "llm-api-key", os.Getenv("ARGUS_LLM_API_KEY"), "LLM API key")
	a.StringVar(&opts.llmModel, "llm-model", os.Getenv("ARGUS_LLM_MODEL"), "LLM model name (overrides config template default)")
	a.DurationVar(&opts.llmTimeout, "llm-timeout", 5*time.Minute, "LLM request timeout")

	if err := a.Parse(args); err != nil {
		return opts, fmt.Errorf("parse flags: %w", err)
	}

	opts.showHelp = a.showHelp
	if opts.showHelp {
		return opts, nil
	}

	if opts.llmBaseURL == "" {
		return opts, fmt.Errorf("--llm-url is required or set ARGUS_LLM_BASE_URL env var")
	}
	if opts.llmAPIKey == "" {
		return opts, fmt.Errorf("--llm-api-key is required or set ARGUS_LLM_API_KEY env var")
	}

	modeCount := 0
	if opts.from != "" || opts.to != "" {
		modeCount++
	}
	if opts.commit != "" {
		modeCount++
	}
	if modeCount == 0 {
		return opts, fmt.Errorf("either --from/--to or --commit is required")
	}
	if modeCount > 1 {
		return opts, fmt.Errorf("only one review mode allowed (--from/--to or --commit)")
	}
	if opts.from != "" && opts.to == "" {
		return opts, fmt.Errorf("--to is required when --from is specified")
	}

	return opts, nil
}

func printReviewUsage() {
	fmt.Println(`Argus - Code Review Agent CLI

Usage:
  argus review [flags]
  argus r [flags]              (alias)

Examples:
  # Review a branch against its base
  argus review --from master --to dev-ref

  # Review a specific commit
  argus review --commit abc123
  argus review -c abc123

  # Output JSON format
  argus review --format json
  argus review -f json

Flags:`)
	// Build a fresh flagset for printing defaults
	fs := flag.NewFlagSet("print", flag.ContinueOnError)
	var d reviewOptions
	fs.StringVar(&d.configPath, "config", "argus.yaml", "path to YAML config file")
	fs.StringVar(&d.rulePath, "rule", "", "path to JSON file with system review rules")
	fs.StringVar(&d.repoDir, "repo", "", "root directory of the git repository (default: current dir)")
	fs.StringVar(&d.from, "from", "", "source ref to start diff from (e.g., 'main')")
	fs.StringVar(&d.to, "to", "", "target ref to end diff at (e.g., 'feature-branch')")
	fs.StringVar(&d.commit, "commit", "", "single commit hash or tag to review (vs its parent) (shorthand: -c)")
	fs.StringVar(&d.outputFormat, "format", "text", "output format: text or json (shorthand: -f)")
	fs.BoolVar(&d.dryRun, "dry-run", false, "run review without submitting comments (testing mode)")
	fs.IntVar(&d.concurrency, "concurrency", 4, "max concurrent file reviews")
	fs.IntVar(&d.perFileTimeout, "timeout", 10, "per-file timeout in minutes")
	fs.StringVar(&d.llmBaseURL, "llm-url", "", "LLM service base URL (OPENAI_COMPATIBLE)")
	fs.StringVar(&d.llmAPIKey, "llm-api-key", "", "LLM API key")
	fs.StringVar(&d.llmModel, "llm-model", "", "LLM model name (overrides config template default)")
	fs.DurationVar(&d.llmTimeout, "llm-timeout", 5*time.Minute, "LLM request timeout")
	fs.PrintDefaults()
	fmt.Println(`
Environment Variables:
  ARGUS_LLM_BASE_URL    LLM service base URL (OpenAI-compatible endpoint)
  ARGUS_LLM_API_KEY     LLM API bearer token
  ARGUS_LLM_MODEL       Default model name`)
}

// --- config subcommand ---

type configAction struct {
	subCmd string // "set"
	key    string
	value  string
}

func parseConfigArgs(args []string) (configAction, error) {
	if len(args) == 0 {
		return configAction{}, fmt.Errorf("usage: argus config set <key> <value>\ne.g., argus config set llm.provider idealab")
	}

	subCmd := args[0]
	switch subCmd {
	case "set":
		if len(args) < 3 {
			return configAction{}, fmt.Errorf("usage: argus config set <key> <value>\ne.g., argus config set llm.model claude-opus-4-6")
		}
		return configAction{
			subCmd: "set",
			key:    args[1],
			value:  args[2],
		}, nil
	default:
		return configAction{}, fmt.Errorf("unknown config sub-command: %s\nAvailable: set", subCmd)
	}
}

func printConfigUsage() {
	fmt.Println(`Configuration management.

Usage:
  argus config set <key> <value>

Examples:
  argus config set llm.provider idealab
  argus config set llm.url https://xx/v1/openai/chat/completions
  argus config set llm.model claude-opus-4-6
  argus config set llm.key xxxxxxxxxx`)
}
