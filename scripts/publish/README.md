# 自动化发布流程

## 概述

一键完成：构建二进制 → 上传到 internal-release repo → 同步版本号 → 发布 npm。

```bash
./scripts/publish/publish-all.sh
```

## 前置要求

- 已安装 `go`、`jq`、`tnpm`、`shasum`
- 已通过 `tnpm login` 登录内部 npm registry
- 已打 git tag（如 `git tag -a v0.1.2 -m "v0.1.2" && git push origin --tags`）
- 工作树干净（无未提交变更）

## 用法

### 常规发布

```bash
./scripts/publish/publish-all.sh
```

### Dev / 预发布版本（无需 git tag）

```bash
OCR_VERSION_OVERRIDE=v0.1.2-dev.1 ./scripts/publish/publish-all.sh
```

### 跳过特定步骤

| 变量 | 作用 |
|------|------|
| `OCR_SKIP_BUILD=1` | 跳过构建（使用已有 dist/ 产物） |
| `OCR_SKIP_INTERNAL=1` | 跳过上传到 internal-release repo |
| `OCR_SKIP_NPM=1` | 跳过 tnpm publish |

```bash
# 仅发 npm，不重新构建和上传
OCR_SKIP_BUILD=1 OCR_SKIP_INTERNAL=1 ./scripts/publish/publish-all.sh
```

### CI / 无交互模式

```bash
OCR_FORCE_YES=1 ./scripts/publish/publish-all.sh
```

## 环境变量

| 变量 | 说明 |
|------|------|
| `OCR_VERSION_OVERRIDE` | 指定版本号（默认取最新 git tag） |
| `OCR_INTERNAL_RELEASE` | 覆盖 internal-release repo 路径（当前使用临时 clone，此变量仅兼容旧脚本） |
| `OCR_FORCE_YES` | 跳过 y/n 确认提示 |

## 执行流程

```
publish-all.sh
  ├── make dist                          ← 构建 4 平台二进制 + checksum
  ├── sync-version.sh                    ← package.json.version = git tag (去 v)
  ├── copy-to-internal-repo.sh           ← 临时 clone internal-release → cp → commit → push → 清理
  ├── update-changelog.sh                ← git log >> NPM-README.md
  ├── publish-npm.sh                     ← tnpm view 检查 → tnpm publish
  └── commit                             ← 自动提交版本变更到主仓库
```
