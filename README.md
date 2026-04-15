# Argus

An open-source, AI-powered code review CLI tool. Argus reads git diffs, analyzes changes through configurable LLM services (OpenAI, Claude, local models), and generates structured code review comments — without depending on any specific code platform.

## Architecture

```
┌─────────────────────────────────────────────────┐
│                    argus CLI                     │
├──────────┬──────────────┬───────────────────────┤
│  Git     │  Agent Core  │  Tool Registry         │
│  Diff    │  (Plan +     │  (file.read,           │
│  Parser  │   Main Loop) │   file.search, etc.)   │
└────┬─────┴──────┬───────┴───────┬───────────────┘
     │            │               │
     ▼            ▼               ▼
  ┌──────┐   ┌──────────┐   ┌──────────────┐
  │ git  │   │ LLM      │   │ User-defined │
  │ diff │   │ Client   │   │ Providers    │
  └──────┘   │(OpenAI-  │   └──────────────┘
             │ comp.)   │
             └──────────┘
```

### Design Principles

1. **Platform-agnostic**: Works with any git repository — no GitHub/GitLab/Lark dependencies required
2. **Bring your own LLM**: Configure any OpenAI-compatible endpoint (Anthropic, OpenAI, Ollama, vLLM, etc.)
3. **Extensible tools**: Register your own implementations for file reading, code search, comment submission, etc.
4. **Security-first**: All data stays within your infrastructure when using self-hosted LLMs

## Quick Start

```bash
# 1. Build
make build

# 2. Create config
cp internal/config/example_config.yaml ./argus.yaml
# Edit model names, prompts, thresholds...

# 3. Run review on staged changes
./bin/argus --staged \
  --llm-url "https://api.openai.com/v1" \
  --llm-api-key "$OPENAI_API_KEY" \
  --llm-model "gpt-4o"

# 4. Or review a ref range
./bin/argus --base main --head feature-branch \
  --config argus.yaml
```

## Configuration

| Flag | Env Var | Description |
|------|---------|-------------|
| `--config` | — | Path to YAML template (prompts, thresholds) |
| `--repo` | — | Git repo root (default: current directory) |
| `--staged` | — | Review staged changes |
| `--base` | — | Base ref for diff range |
| `--head` | — | Head ref for diff range |
| `--llm-url` | `ARGUS_LLM_BASE_URL` | OpenAI-compatible API endpoint |
| `--llm-api-key` | `ARGUS_LLM_API_KEY` | Bearer token |
| `--llm-model` | `ARGUS_LLM_MODEL` | Model name override |
| `--format` | — | Output format: `text` or `json` |
| `--dry-run` | — | Run without submitting comments |
| `--concurrency` | — | Max concurrent file reviews |
| `--timeout` | — | Per-file timeout (minutes) |

## Extending Tools

Argus defines six extension points via the `tool.Provider` interface:

| Tool | Alias | Purpose |
|------|-------|---------|
| `FileRead` | `file_read` | Read file content at path/line range |
| `FileFind` | `file_find` | Find files by glob pattern |
| `FileReadDiff` | `file_read_diff` | Get diff between refs |
| `FileSearch` | `file_search` | Search text within files (grep-like) |
| `CodeSearch` | `code_search` | Semantic/symbol-aware code search |
| `CodeComment` | `code_comment` | Submit review comments to platform |

See `internal/config/example_tools.go` for stub implementations showing how to wire up each one. Register them in `buildToolRegistry()` in `cmd/argus/main.go`.

## Project Structure

```
Argus/
├── cmd/argus/
│   ├── main.go          # Entry point & orchestration
│   ├── flags.go          # CLI flag parsing
│   ├── git.go            # Git command helpers
│   └── output.go         # Text/JSON output formatters
├── internal/
│   ├── agent/
│   │   └── agent.go      # Core review agent (Plan + Main Loop)
│   ├── config/
│   │   ├── template.go   # Template YAML struct definitions
│   │   ├── example_config.yaml  # Sample configuration
│   │   └── example_tools.go     # Tool implementation stubs
│   ├── diff/
│   │   ├── parser.go     # Unified diff parser
│   │   └── git.go        # Git diff execution helpers
│   ├── llm/
│   │   └── client.go     # OpenAI-compatible LLM client
│   ├── model/
│   │   └── diff.go       # Data models (Diff, LlmComment, etc.)
│   └── tool/
│       ├── definitions.go # Tool enum + Provider interface + Registry
│       ├── response_message.go  # Checkpoint + result types
│       └── stub.go       # Stub/builtin providers
├── pkg/                   # Public API (future)
├── Makefile
├── go.mod
└── README.md
```

## License

MIT
