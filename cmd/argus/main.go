// Argus is an AI-powered code review CLI tool.
// It reads git diffs, sends them to a configurable LLM service, and generates review comments.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/argus-review/argus/internal/telemetry"
)

func main() {
	ctx := context.Background()
	if telemetry.Init(ctx) {
		defer telemetry.ShutdownWithTimeout(ctx, 5*time.Second)
	}

	if err := dispatch(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// dispatch routes top-level subcommands or global flags.
func dispatch() error {
	args := os.Args[1:]

	// No args → default to review with empty args (will trigger usage/help)
	if len(args) == 0 {
		printTopLevelUsage()
		return nil
	}

	switch args[0] {
	case "--version", "-v":
		printVersion()
		return nil
	case "version":
		printVersion()
		return nil
	case "review", "r":
		return runReview(args[1:])
	case "config":
		return runConfig(args[1:])
	case "llm":
		return runLLM(args[1:])
	case "-h", "--help":
		printTopLevelUsage()
		return nil
	default:
		return fmt.Errorf("unknown command: %s\nRun 'argus' for usage", args[0])
	}
}

func printTopLevelUsage() {
	fmt.Println(`Argus - Code Review Agent CLI

Usage:
  argus [command]

Commands:
  review, r    Start a code review
  config       Manage configuration settings
  llm          LLM utility commands
  version      Show version information

Examples:
  argus review --from dev --to master      Review diff range
  argus review --commit abc123             Review a single commit
  argus config set llm.model opus-4-6      Set a config value
  argus llm test                           Test LLM connectivity
  argus version                            Show version info

Use "argus review -h" for more information about review.
Use "argus config" for more information about config.
Use "argus llm" for more information about LLM utilities.`)
}
