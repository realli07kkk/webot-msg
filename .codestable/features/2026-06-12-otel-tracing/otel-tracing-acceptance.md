# otel-tracing 验收报告

> 阶段：阶段 3（验收闭环）  
> 验收日期：2026-06-12  
> 关联方案 doc：`.codestable/features/2026-06-12-otel-tracing/otel-tracing-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：

- [x] `runtimeconfig.TelemetryConfig`（`internal/runtimeconfig/config.go:73`）：TOML `[telemetry]` 支持 `endpoint` / `protocol` / `insecure` / `service_name` / `headers` / `resource_attributes`，默认 `grpc` 与 `webot-msg`；`TestLoadFileAcceptsTelemetrySection`、`TestLoadFileMergesDefaults` 覆盖一致。
- [x] `telemetry.Config` / `telemetry.Setup`（`internal/telemetry/telemetry.go:27`、`:36`）：`Endpoint` 空返回 no-op shutdown；非 `grpc/http` 返回 `telemetry.protocol` error；正常路径返回可 flush 的 shutdown；`TestSetupDisabledReturnsNoopShutdown`、`TestSetupRejectsInvalidProtocol`、`TestSetupHTTPExporterSendsHeadersAndResourceAttributes` 覆盖一致。
- [x] `ilink.Client`（`internal/ilink/client.go:26`、`:52`）：构造时持有共享 `http.RoundTripper`，新增 context-aware 调用，旧无 context 方法保留；`TestClientUsesInjectedSharedTransportWithPerCallTimeouts` 覆盖一致。

**名词层“现状 → 变化”逐项核对**：

- [x] Runtime config 从六个 TOML section 增加可选 `[telemetry]` section，严格未知 key 语义不变。
- [x] 新增 `internal/telemetry` 作为单一 SDK 初始化节点，`main` 只做配置转换和 shutdown 编排。
- [x] `api.Server` 从裸 mux 变为 mux 外层包 `otelhttp`，业务 handler 仍收到原请求语义。
- [x] `ilink.Client` 从每方法临时默认 transport 变为共享可注入 transport，各方法 timeout 语义保留。

**流程图核对**：

- [x] 启动图节点已落地：Runtime config → `telemetry.Setup` → `app.New` / `app.Run`，代码锚点 `cmd/webot-msg/main.go:30`、`:61`、`:78`。
- [x] 请求图节点已落地：`otelhttp` server → `handleBotAction` → `sender.SendProtectedText` → `ilink.SendMessageContext` → OTLP exporter，代码锚点 `internal/api/server.go:62`、`:82`、`internal/sender/protected_text.go:22`、`internal/ilink/client.go:194`、`internal/telemetry/telemetry.go:57`。

结论：接口契约与方案一致，无需回填 design 或继续修代码。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] API 入站与 iLink 出站产生 traces：`TestTelemetryE2EExportsInboundAndOutboundSpansWithSameTrace` 验证 server/client spans 同 trace_id。
- [x] OTLP 厂商中立：生产依赖只使用 OpenTelemetry 官方 OTLP traces exporter 与 `otelhttp`，无厂商私有 SDK。
- [x] 未配置时兼容旧行为：`TestSetupDisabledReturnsNoopShutdown` 与 `TestTelemetryDisabledDoesNotPropagateTraceparent` 验证 disabled 路径无 traceparent 传播。

**明确不做逐项核对**：

- [x] metrics/logs 未做：`rg "otlpmetric|otlplog"` 未命中生产代码和 `go.mod`。
- [x] 长轮询 / Redis / 控制台手工 span 未做：生产代码里无业务手写 `Tracer().Start`；iLink transport filter 只在 context 已有有效 span 时启用。
- [x] `OTEL_*` 配置入口未做：生产代码无 `os.Getenv("OTEL`；`OTEL_EXPORTER_OTLP*` 字符串只用于屏蔽官方 exporter 环境变量读取。
- [x] 采样率配置未做：`rg "sample_rate|sampling|sampler"` 未命中生产配置。
- [x] 厂商私有 SDK 未做：`go.mod` 无腾讯云、Datadog、New Relic、Jaeger、Zipkin、Tempo 等私有 SDK 依赖。

**关键决策落地**：

- [x] 默认关闭、按配置启用：`telemetry.Setup` endpoint 空直接 no-op；`runtimeconfig.Default` endpoint 空。
- [x] 认证用通用 maps：`Headers` 与 `ResourceAttributes` 直接从 TOML map 进入 exporter/resource。
- [x] 埋点常驻、provider 切换：API middleware 与 iLink transport 无条件挂载，disabled 由 noop provider/filter 控制。
- [x] 协议默认 gRPC、可选 HTTP：`DefaultTelemetryProtocol = "grpc"`，`resolveTelemetry` 只接受 `grpc/http`。
- [x] 配置非法快速失败、运行期导出失败不影响业务：配置解析返回具体 key error；不可达 endpoint 场景 API 仍返回 200。
- [x] TOML-only 配置边界：`withOTLPExporterEnvDisabled` 在构造 exporter 时屏蔽并恢复 `OTEL_EXPORTER_OTLP*`，CR-001 已关闭。

**编排层“现状 → 变化”逐项核对**：

- [x] `main` 在 `PrepareStorage` 后、`app.New` 前插入 `telemetry.Setup`，失败走 `fatalStartupError`，defer shutdown 带 5s timeout。
- [x] `api.Server.handler` 外层包 `instrumentedHandler`，query 脱敏后交给 `otelhttp.NewHandler`。
- [x] API 发送路径把请求 context 传到 `sender.SendProtectedText` 和 `ilink.SendMessageContext`。
- [x] iLink 出站统一经共享 instrumented transport，各方法仍创建带各自 timeout 的 `http.Client`。

**流程级约束核对**：

- [x] 配置非法错误包含具体配置项：`telemetry.protocol`、`telemetry.endpoint`、`telemetry.endpiont` 都有单测证据。
- [x] shutdown 有上限：`telemetryShutdownTimeout = 5 * time.Second`，defer 中使用 `context.WithTimeout`。
- [x] 敏感信息不进 span：`TestTelemetrySpansOmitSensitiveRequestValues` 覆盖 API token、消息正文、bot token、context token。
- [x] 启动日志不打印 headers/resource values：`main` 只打印 endpoint、protocol、service_name、insecure。

**挂载点反向核对（可卸载性）**：

- [x] 挂载点 1：`internal/runtimeconfig/config.go` 新增 `[telemetry]` section 与校验。
- [x] 挂载点 2：`cmd/webot-msg/main.go` 新增 `telemetry.Setup` 与 shutdown。
- [x] 挂载点 3：`internal/api/server.go` 新增入站 `otelhttp` middleware。
- [x] 挂载点 4：`internal/ilink/client.go` 新增共享 instrumented transport 与 context-aware 方法。
- [x] 挂载点 5：`go.mod` 新增 `go.opentelemetry.io/*` traces 相关依赖。
- [x] 反向核查：除测试与 CodeStable 文档外，生产引用集中在上述挂载点及必要的 context 传递支持（`internal/app`、`internal/sender`），未出现方案外 telemetry 子系统。
- [x] 拔除沙盘推演：移除五个挂载点后，API/iLink tracing 能力消失；`internal/app` / `internal/sender` 的 context-aware 接口调整是出站挂载点的支撑代码，可随出站挂载点一并回退。

结论：行为和决策与方案一致。

## 3. 验收场景核对

- [x] **S1**：endpoint 指向本地 OTLP-compatible receiver，`POST /bots/{botID}/messages` 成功。
  - 证据来源：集成单测 `TestTelemetryE2EExportsInboundAndOutboundSpansWithSameTrace`
  - 结果：通过；collector 收到 server span 和 iLink client span，trace_id 相同。
- [x] **S2**：`[telemetry]` 缺失或 endpoint 为空。
  - 证据来源：单测 `TestSetupDisabledReturnsNoopShutdown`、`TestTelemetryDisabledDoesNotPropagateTraceparent`
  - 结果：通过；未配置时不传播 traceparent，业务返回保持成功。
- [x] **S3**：`resource_attributes` 配置 token。
  - 证据来源：单测 `TestSetupHTTPExporterSendsHeadersAndResourceAttributes`
  - 结果：通过；导出 resource 含 `token` 与自定义 `env`。
- [x] **S4**：`headers` 配置任意 header。
  - 证据来源：单测 `TestSetupHTTPExporterSendsHeadersAndResourceAttributes`
  - 结果：通过；OTLP HTTP 请求携带 TOML header。
- [x] **S5**：非法 protocol 或 telemetry 未知 key。
  - 证据来源：单测 `TestResolveRejectsInvalidTelemetryConfig`、`TestLoadFileRejectsUnknownKeys`、`TestSetupRejectsInvalidProtocol`
  - 结果：通过；错误包含具体配置项。
- [x] **S6**：endpoint 不可达时调 API 发消息。
  - 证据来源：集成单测 `TestTelemetryExportFailureDoesNotBlockAPIResponse`
  - 结果：通过；API 返回 200。
- [x] **S7**：启用 telemetry 后前台 Ctrl+C 退出。
  - 证据来源：静态代码证据 `internal/app/app.go:151`、`cmd/webot-msg/main.go:69`
  - 结果：通过；前台 Ctrl+C 使 `app.Run` 返回，defer shutdown 使用 5s timeout，不存在无限等待路径。
- [x] **S8**：`insecure = true` + 本地无 TLS collector。
  - 证据来源：HTTP collector 单测 `TestSetupHTTPExporterSendsHeadersAndResourceAttributes` 与 E2E 测试均使用 `Insecure: true`
  - 结果：通过；明文 OTLP HTTP 上报成功。

**反向核对项**：

- [x] `go.mod` 中未引入 `otlpmetric`、`otlplog` 或厂商私有 telemetry SDK。
- [x] 生产代码中未引入 `os.Getenv("OTEL` 配置入口。
- [x] 导出 span 不含消息正文、BotToken、APIToken、ContextToken；服务端 span URL 去 query。
- [x] 启动日志不打印 `[telemetry.headers]` 或 `resource_attributes` 的值。
- [x] 不存在采样率相关配置 key。

最终验证命令：

- [x] `go test ./...`
- [x] `go vet ./...`
- [x] `git diff --check`
- [x] `python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-06-12-otel-tracing/otel-tracing-checklist.yaml`
- [x] `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://env.example/custom go test ./internal/telemetry -run TestSetupHTTPExporterSendsHeadersAndResourceAttributes -count=1`

## 4. 术语一致性

- Telemetry：代码中作为 runtime config section、internal package 和 architecture 术语使用，语义一致。
- endpoint：`telemetry.endpoint` 表示 OTLP `host:port`，未与 `ilink.base_url` 混用。
- 厂商中立：依赖限定在 OpenTelemetry 官方模块和标准 OTLP；无厂商私有 SDK。
- OTel SDK shutdown：`telemetry.Setup` 返回 shutdown，`main` 负责限时调用。
- 防冲突：`OTEL_EXPORTER_OTLP*` 只作为官方 exporter env 屏蔽规则出现，不作为配置名词外扩。

结论：术语一致，无命名漂移。

## 5. 架构归并

对照方案第 4 节，已实际更新 `.codestable/architecture/ARCHITECTURE.md`：

- [x] 名词归并：术语节新增 `Telemetry`，说明 `[telemetry]`、默认关闭、OTLP traces、TOML-only 和 env 屏蔽边界。
- [x] 动词骨架归并：结构与交互节补充 `main` 启动序列中的 `telemetry.Setup` / shutdown，新增 `internal/telemetry` 包，补充 API 入站和 iLink 出站 tracing 流程。
- [x] 流程级约束归并：关键决策节新增 telemetry 默认关闭、TOML-only、traces-only、导出失败不影响业务；已知约束节新增敏感信息、日志和 `OTEL_EXPORTER_OTLP*` env 屏蔽约束。
- [x] 相关文档归并：架构 doc frontmatter `implements` 新增 `service-observability`，相关文档节链接新 requirement。

判据：没读过 design 的人只看 architecture，也能知道系统已有可选 OTel traces、配置入口、入站/出站挂载点和安全边界。

## 6. requirement 回写

- [x] `requirement` frontmatter 为空，但本次新增运维者可感的可观测能力，因此触发 `cs-req backfill`。
- [x] 已新增 `.codestable/requirements/service-observability.md`，状态为 `current`，描述 APM 链路可见性、默认关闭、厂商中立、TOML-only 和敏感信息边界。
- [x] 已更新 `.codestable/requirements/VISION.md`，把 `service-observability` 加入 Current 列表，并刷新 `last_reviewed`。

## 7. roadmap 回写

- [x] 方案 frontmatter 未设置 `roadmap` / `roadmap_item`，本 feature 非 roadmap 起头。
- [x] 跳过 roadmap items.yaml 和主文档同步。

## 8. attention.md 候选盘点

- [x] 有候选，不在 acceptance 阶段擅自写入。

候选：

- OpenTelemetry 官方 OTLP exporter 会默认读取 `OTEL_EXPORTER_OTLP*` 环境变量；本项目 telemetry 配置必须保持 TOML-only，因此构造 exporter 时要屏蔽这些 env，不能让它们成为第二配置源。建议放入 `.codestable/attention.md` 的“项目约定”或“工具 / 环境注意事项”。

## 9. 遗留

- 后续优化点：可按需补真实 OpenTelemetry Collector 进程级手工验收记录；当前自动化证据使用本地 OTLP-compatible `httptest` receiver。
- 已知限制：测试为解析 OTLP protobuf 直接依赖 `go.opentelemetry.io/proto/otlp` 与 `google.golang.org/protobuf`；生产路径未直接使用这两个解析依赖。
- 实现阶段顺手发现：无需要单独开 issue 的缺陷。
- 用户终审：待用户确认。
