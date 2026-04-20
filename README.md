# Argus

AI-powered code review CLI tool built in Go. Argus reads git diffs, sends changed files to a configurable LLM (via OpenAI-compatible API), and generates structured code review comments — acting as an autonomous AI reviewer that can read your full project context, not just the diff.

## Features

- **Three review modes**: workspace changes (default), branch range (`--from` / `--to`), single commit (`--commit` / `-c`)
- **Context-aware reviews**: the LLM agent can read arbitrary files, search code with `git grep`, and inspect diffs — going beyond surface-level diff analysis
- **Plan phase**: for large changes (>50 lines), the agent first produces a risk analysis plan before diving into detailed review
- **Memory compression**: automatically summarizes conversation context when token count exceeds threshold (~50K tokens) so long reviews don't run out of context
- **Concurrent execution**: each file reviewed independently in parallel (configurable concurrency, default 4 workers)
- **Customizable rules**: glob-based system rules let you apply different review standards per language or path pattern
- **Multiple output formats**: text (default) or JSON
- **Any OpenAI-compatible LLM**: works with OpenAI, Claude (via compatible endpoint), local models, etc.

## Installation

### Build from Source

Requires [Go](https://go.dev/) 1.24+.

```bash
git clone https://github.com/argus-review/argus.git
cd argus
make        # builds binary at ./bin/argus
```

Or build directly:

```bash
go build -o bin/argus ./cmd/argus
```

Add `bin/` to your `$PATH` or copy the binary somewhere accessible.

## Quick Start

### 1. Configure your LLM

Argus connects to any OpenAI-compatible API endpoint. Set up your credentials:

```bash
argus config set llm.url https://your-api-endpoint/v1/chat/completions
argus config set llm.auth_token your-api-key-here
argus config set llm.model claude-opus-4-6
```

Configuration is stored at `~/.argus/config.json`.

### 2. Test connectivity

```bash
argus llm test
```

### 3. Run a review

Navigate to any git repository and run:

```bash
# Review all staged, unstaged, and untracked changes
argus review

# Review differences between two branches
argus review --from main --to feature-branch

# Review a specific commit
argus review --commit abc123
```

## Usage

### Commands

| Command | Description |
|---------|-------------|
| `argus review` / `argus r` | Start a code review session |
| `argus config set <key> <value>` | Manage user configuration |
| `argus llm test` | Test LLM connectivity |
| `argus version` | Show version information |

### Review Flags

| Flag | Shorthand | Default | Description |
|------|-----------|---------|-------------|
| `--repo` | | current dir | Root directory of the git repository |
| `--from` | | | Source ref to start diff from (e.g., `main`) |
| `--to` | | | Target ref to end diff at (e.g., `feature-branch`) |
| `--commit` | `-c` | | Single commit hash to review (vs its parent) |
| `--format` | `-f` | `text` | Output format: `text` or `json` |
| `--concurrency` | | `4` | Max concurrent file reviews |
| `--timeout` | | `10` | Per-file timeout in minutes |
| `--rule` | | | Path to JSON file with custom system review rules |
| `--tools` | | embedded | Path to JSON tools config file (uses embedded defaults if omitted) |

### Review Modes

**Workspace mode** (no flags): reviews all staged, unstaged, and untracked changes in the current working directory.

```bash
argus review
```

**Branch range mode**: reviews changes between two git refs using merge-base.

```bash
argus review --from main --to dev
```

**Single commit mode**: reviews a specific commit against its parent.

```bash
argus review --commit abc123
```

## Configuration

User config lives at `~/.argus/config.json`. Supported keys:

| Key | Description | Example |
|-----|-------------|---------|
| `llm.provider` | Provider name (informational) | `openai`, `claude` |
| `llm.url` | OpenAI-compatible API endpoint URL | `https://api.openai.com/v1/chat/completions` |
| `llm.auth_token` | API key / authentication token | `sk-xxx...` |
| `llm.model` | Model identifier | `gpt-4o`, `claude-opus-4-6` |
| `language` | Preferred response language | `zh-CN`, `en-US` |

Example config:

```json
{
  "llm": {
    "provider": "claude",
    "url": "https://api.anthropic.com/v1/messages",
    "auth_token": "your-api-key",
    "model": "claude-opus-4-6"
  },
  "language": "Chinese"
}
```

## How It Works

```
┌─────────────┐
│  Load Diff   │ Parse git unified diffs into structured per-file changes
└──────┬──────┘
       ▼
┌─────────────┐
│ Plan Phase   │ For large files (>50 lines changed), LLM produces a risk
│ (optional)   │ analysis plan identifying severity and recommended actions
└──────┬──────┘
       ▼
┌─────────────┐
│ Main Loop    │ Multi-turn LLM conversation — the agent calls tools to read
│              │ files, search code, inspect diffs, and submit review comments
└──────┬──────┘
       ▼
┌─────────────┐     ┌─────────────┐
│  Compress    │ When context exceeds ~50K tokens, prior conversation is
│  Memory      │ summarized to stay within limits
└──────┬──────┘
       ▼
┌─────────────┐
│  Output      │ Collected comments rendered as text or JSON
└─────────────┘
```

Each changed file is reviewed as an independent subtask running concurrently (configurable semaphore). Full session history (requests, responses, tool calls, durations, token estimates) is saved to `<repo>/temp/argus-session-*.json` for debugging.

### LLM Agent Tools

During a review, the AI agent has access to these tools:

| Tool | Description |
|------|-------------|
| `file_read` | Read file contents at a given path (with optional line range, max 500 lines per call) |
| `file_find` | Find files by name pattern across the repository (skips node_modules, vendor, .idea, etc.) |
| `code_search` | Search code content using `git grep` (supports regex, case-insensitive, file-pattern scoping) |
| `file_read_diff` | Read the diff for a specific file |
| `code_comment` | Submit a review comment with suggestion code, line ranges, and reasoning |
| `task_done` | Signal that the review for a file is complete |

## Project Structure

```
├── cmd/argus/          # CLI entry point and command dispatch
│   ├── main.go         # Application entry point
│   ├── review_cmd.go   # Core review orchestration
│   ├── flags.go        # Flag parsing
│   ├── config_cmd.go   # Config management
│   ├── llm_cmd.go      # LLM utility commands
│   └── ...
├── internal/
│   ├── agent/          # Review pipeline orchestrator (diff loading → plan → main loop → memory compression)
│   ├── config/         # Templates, rules, tool definitions
│   ├── diff/           # Git diff parsing and resolution
│   ├── llm/            # OpenAI-compatible LLM client with retry/backoff
│   ├── model/          # Data models
│   ├── session/        # Session history tracking
│   └── tool/           # Tool implementations (file_read, code_search, etc.)
└── pkg/                # Public package exports
```

## License

See [LICENSE](LICENSE) for details.
