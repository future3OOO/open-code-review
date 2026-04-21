# Plan: 为 Argus CLI 添加 OpenTelemetry Telemetry

## Context

Argus 是一个 AI 驱动的 Code Review CLI，面向两类用户：
1. **个人开发者** — 本地使用时希望看到评审过程的实时进度和耗时
2. **系统集成方** — 将 argus 作为评审组件嵌入更大系统，需要结构化 trace 数据上报到 APM 后端

参考 Claude Code 的 OTEL 集成模式，采用标准 `OTEL_*` 环境变量驱动 + Console 默认导出 + OTLP 可选导出 的方案，一套代码覆盖两个场景。

## 架构概览

新增 `internal/telemetry/` 包，使用全局变量 + no-op 默认值，零侵入当未启用时：

```
internal/telemetry/
├── config.go       # 配置解析（环境变量 + config.json telemetry 段）
├── provider.go     # TracerProvider / MeterProvider / LoggerProvider 初始化
├── span.go         # Span 辅助函数（StartSpan、属性设置等）
├── metrics.go      # 指标记录封装
├── events.go       # 结构化事件日志（替换 fmt.Printf("[argus]...)")
└── shutdown.go     # 优雅关闭
```

## 配置方案

### 环境变量（优先级最高）

| 变量 | 用途 | 默认 |
|------|------|------|
| `ARGUS_ENABLE_TELEMETRY=1` | 总开关 | unset = 禁用 |
| `OTEL_SERVICE_NAME` | 服务名 | `"argus"` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP collector 地址 | unset → 回退 console |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | grpc 或 http/protobuf | `"grpc"` |
| `ARGUS_CONTENT_LOGGING=1` | 包含 prompt/response 内容 | false (仅元数据) |

### config.json 扩展（低优先级，被环境变量覆盖）

在 `$HOME/.argus/config.json` 中增加：
```json
{
  "telemetry": {
    "enabled": true,
    "exporter": "otlp",
    "otlp_endpoint": "http://localhost:4317",
    "content_logging": false
  }
}
```

配置读取优先级：**默认值 < config.json < 环境变量**

## 依赖变更

`go.mod` 新增：
```bash
go get go.opentelemetry.io/otel@latest
go get go.opentelemetry.io/otel/sdk@latest
go get go.opentelemetry.io/otel/exporters/stdout/stdouttrace@latest
go get go.opentelemetry.io/otel/exporters/stdout/stdoutmetric@latest
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@latest
go get go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc@latest
```

## 核心文件修改清单

### 1. `cmd/argus/main.go` — 生命周期钩子

在 `main()` 中添加 Init/Shutdown（约 +8 行）：
```go
func main() {
    ctx := context.Background()
    init, _ := telemetry.Init(ctx)
    if init { defer telemetry.ShutdownWithTimeout(ctx, 5*time.Second) }

    if err := dispatch(); err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
}
```

### 2. `cmd/argus/review_cmd.go` — 根 Span

- `runReview()` (L102): 创建根 span `review.run`
  ```go
  ctx, span := telemetry.StartSpan(context.Background(), "review.run")
  defer span.End()
  // 后续 ag.Run(ctx) 传入此 ctx
  ```
- 结束时记录 summary 指标（文件数、评论数、总耗时）

### 3. `internal/agent/agent.go` — 主要埋点文件

这是 instrumentation 最密集的文件：

| 位置 | 埋点 | Span 名 |
|------|------|---------|
| `Run()` L162 | diff 加载 | `diff.parse` |
| `Run()` L169 | 事件 | `no.files.changed` |
| `Run()` L176 | 事件 | `review.started` (file count, repo dir) |
| `dispatchSubtasks()` L232 | 整体计时 | histogram: `review.duration_seconds` |
| `executeSubtask()` L277 | per-file | `subtask.execute.{filepath}` |
| `executeSubtask()` L264 | 事件 | `subtask.error` |
| `executeSubtask()` L295 | 事件 | `plan.skipped` |
| `executeSubtask()` L300 | 事件 | `plan.failed` |
| `executePlanPhase()` L375 | span | `phase.plan` |
| `performLlmCodeReview()` L446 | span | `phase.main_loop` |
| `performLlmCodeReview()` L462-471 | metrics | `llm.requests_total`, `llm.request_duration_seconds`, `llm.tokens_used` |
| `performLlmCodeReview()` L478 | 事件 | `llm.no.tool.calls` |
| `executeToolCall()` L541 | span | `tool.execute.{name}` + metrics: `tool.calls_total`, `tool.execution_duration_seconds` |
| `compressAndRecord()` L658 | span | `phase.memory_compression` |

所有现有的 `fmt.Printf("[argus] ...")` 保留给用户可见输出，同时追加对应的 `telemetry.Event()` 调用。

### 4. `cmd/argus/config_cmd.go` — 配置结构扩展

`Config` struct (L69) 增加 telemetry 字段：
```go
type Config struct {
    Llm       LlmConfig       `json:"llm,omitempty"`
    Language  string          `json:"language,omitempty"`
    Telemetry *TelemetryConfig `json:"telemetry,omitempty"` // NEW
}

type TelemetryConfig struct {
    Enabled      bool   `json:"enabled,omitempty"`
    Exporter     string `json:"exporter,omitempty"`
    OTLPEndpoint string `json:"otlp_endpoint,omitempty"`
    ContentLog   bool   `json:"content_logging,omitempty"`
}
```

## 设计决策

- **为什么用全局变量而非 DI**：CLI 短进程模型，DI 需要在 Agent.Args、多个方法签名之间传递 telemetry 实例，侵入性大。全局 no-op 默认在未启用时零开销。
- **为什么不改 `llm/client.go`**：LLM client 保持通用干净。所有 LLM 指标从 agent 层的 `SetResponse` 点采集即可。
- **Console vs OTLP 默认**：默认 Console exporter（个人用户直接看终端），设置了 `OTEL_EXPORTER_OTLP_ENDPOINT` 后自动切换 OTLP。
- **短生命周期处理**：CLI 每次执行都是独立进程，metrics 在 Shutdown 时一次性 flush，不用 server 模型的周期性导出。

## 分阶段实施

### Phase 1: 基础设施
1. 安装 OTel Go 依赖
2. `config.go` — 环境变量 + config.json 解析
3. `provider.go` — 初始化 TracerProvider/MeterProvider/LoggerProvider（先只支持 Console exporter）
4. `shutdown.go` — 优雅关闭逻辑
5. `main.go` — 接入 Init/Shutdown
6. 验证：`ARGUS_ENABLE_TELEMETRY=1 argus review --from dev --to main` 能看到 console 输出

### Phase 2: Spans + Events
1. `span.go` — StartSpan / EndSpan helper
2. `events.go` — Event() 函数
3. `review_cmd.go` — 根 span `review.run`
4. `agent.go` — diff.parse / subtask / phase.* spans + 替换 `[argus]` printf 为事件
5. 验证：console 输出能看到完整 span tree

### Phase 3: Metrics
1. `metrics.go` — Counter / Histogram 句柄
2. `agent.go` — 接入 LLM request/token metrics、tool metrics
3. 验证：console 能看到指标输出

### Phase 4: OTLP Exporter + 完善
1. `provider.go` — 根据 `OTEL_EXPORTER_OTLP_ENDPOINT` 切换 OTLP exporter
2. TRACEPARENT 传播
3. `ARGUS_CONTENT_LOGGING` 隐私控制
4. 集成测试：连接本地 Jaeger 验证 trace 显示

## 验证方式

- **Console 模式**：`ARGUS_ENABLE_TELEMETRY=1 argus review --from dev --to main`
- **JSON 模式**：同上，console exporter 默认输出 JSON lines
- **OTLP 模式**：
  ```bash
  docker run -d -p 4317:4317 -p 16686:16686 jaegertracing/all-in-one:latest
  ARGUS_ENABLE_TELEMETRY=1 OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317 argus review --from dev --to main
  # 浏览器打开 http://localhost:16686 查看 trace
  ```
- **禁用模式**（零开销验证）：不设环境变量，确认行为与之前完全一致
