# OpenCodeReview CLI

AI-powered code review tool that reads Git diffs, sends changed files to a configurable LLM via OpenAI-compatible API, and generates structured review comments. It goes beyond surface-level analysis — the Agent can read project context for deep reviews.

## Install

```bash
tnpm install -g @ali/open-code-review
```

After installation, the `ocr` command is available globally.

### Version Control

```bash
# Install specific version
OCR_VERSION=v0.1.0 tnpm install -g @ali/open-code-review
```

## Prerequisites

**You must configure an LLM provider before using `ocr`.** The tool requires access to an OpenAI-compatible API endpoint (OpenAI, Claude, local models, etc.).

```bash
ocr config set llm.url https://your-api-endpoint/v1/chat/completions
ocr config set llm.auth_token your-api-key-here
ocr config set llm.model claude-opus-4-6
```

Or via environment variables:

```bash
export OCR_LLM_URL=https://your-api-endpoint/v1/chat/completions
export OCR_LLM_TOKEN=your-api-key-here
```

Config is stored in `~/.open-code-review/config.json`.

### Test Connectivity

```bash
ocr llm test
```

## Quick Start

Navigate to any Git repository and run:

```bash
# Review all workspace changes
ocr review

# Review diff between two branches
ocr review --from main --to feature-branch

# Review a single commit
ocr review --commit abc123
```

## Commands

| Command | Description |
|---------|-------------|
| `ocr review` / `ocr r` | Start code review |
| `ocr config set <key> <value>` | Manage configuration |
| `ocr llm test` | Test LLM connectivity |
| `ocr viewer` | Start WebUI session viewer |
| `ocr version` | Show version info |

## Common Options

| Flag | Shorthand | Default | Description |
|------|-----------|---------|-------------|
| `--repo` | | current dir | Git repository root |
| `--from` | | | Source ref (e.g., `main`) |
| `--to` | | | Target ref (e.g., `feature-branch`) |
| `--commit` | `-c` | | Review a single commit |
| `--format` | `-f` | `text` | Output format: `text` or `json` |
| `--concurrency` | | `4` | Max concurrent file reviews |
| `--timeout` | | `10` | Per-file timeout (minutes) |

## Features

- **Three review modes**: workspace changes, branch range, single commit
- **Context-aware**: Agent reads arbitrary files, searches code via `git grep`, inspects diffs
- **Plan phase**: Large changes (>50 lines) get risk analysis first
- **Any LLM**: Works with OpenAI, Claude-compatible endpoints, local models
- **Concurrent**: Files reviewed in parallel (configurable workers)

## License

Apache-2.0

---

## Changelog v0.1.2 (2026-05-13)

- feat: 增加发包的 readme (1d4b783)
- feat: 增加内部打包分发的脚本 (fd3b5d3)
- feat: 增加内部打包分发的脚本 (01a809a)
- feat: update --audience agent (8391e1e)
- feat: update output note (dd12b72)
- feat: update scripts/release.sh (1319f3a)
- feat: Adding the  parameter distinguishes between human and agent input. (5f09e5e)
- feat: fix tokens count (b73e05c)
- feat: add TODO note (1135690)
- feat: update Examples (2babfe8)
- feat: 记忆压缩三层分区 + 双阈值异步压 (14adf6f)
- feat: 四层模型端点，适配 anthropic (8a93673)
- feat: 增加一些边界提示 (b680c29)
- feat: 区分没有diff和没有评论的反馈 (a217a6f)
- feat: --format json 模式下静默进度输出，保持 stdout 纯净 (5aea3ac)
- feat: 增加 git 仓库校验 (39d02d1)
- feat: 修改配置项内容 (22d93ed)
- feat: 增加评论块与diff渲染 (d0e4832)
- chore: 移除 .claude 追踪并更新 .gitignore (e99092a)
- feat: 增加viewer能力 (d99e3eb)
- feat: 配置化默认允许评审的文件 (576180d)
- feat: 大diff拦截 (21a4b66)
- feat: add license (2cd09e6)
- feat: 修改文件名 (84df403)
- feat: 更新readme (49cb629)
- feat: token used 展示拆分成 input 和 output (1c76de0)
- feat: 移除历史文件索引 (9589cb6)
- feat: 支持类似claude code 会话历史的能力 (daa7181)
- feat: add .gitignore (edd6854)
- feat: 修复一些问题 (9ecadd7)
- feat: 修复一些问题 (52e035d)
- feat: 修复一些问题 (07b6ba2)
- feat: 修复一些问题 (e2d6a54)
- feat: 增加 --debug模式 (a896ba5)
- feat: 修复评论重复的问题 (223a6ef)
- feat: 改名 (2f2aaa1)
- feat: argus 改名 opencodereview (3d2029d)
- feat: 修复了一些问题 (9e449d5)
