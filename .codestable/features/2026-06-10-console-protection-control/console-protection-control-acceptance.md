---
doc_type: feature-acceptance
feature: 2026-06-10-console-protection-control
status: accepted
summary: 验收控制台运行期保护开关、保护状态查询、旧配置迁移提示与受保护发送事务 generation 边界
tags: [console, protection, redis, config]
accepted_at: 2026-06-10
---

# console-protection-control 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-10
> 关联方案 doc：`.codestable/features/2026-06-10-console-protection-control/console-protection-control-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `/protection enable`：`internal/console/console.go` 解析子命令并调用 `Controller.EnableProtection`；`internal/app/app.go` 使用启动时解析出的 Redis 配置创建 Redis guard，执行 `PING` 后启用并启动检查器。测试：`internal/console/console_test.go`、`internal/protection/runtime_guard_test.go`、`internal/app/send_text_test.go`。
- [x] `/protection disable`：console 调用 `DisableProtection`；app 取消保护窗口检查器，并让 `RuntimeGuard` 后续新 operation 走 no-op。已开始 operation 继续持有旧 generation 收尾。测试：`TestRuntimeGuardOperationKeepsGenerationAfterDisable`、`TestSendProtectedTextReleaseUsesReservedGenerationAfterDisable`、`TestSendProtectedTextReminderRecordUsesReservedGenerationAfterDisable`。
- [x] `/protection status`：console 调用 `PrintProtectionStatus(activeBotID)`；app 只做格式化，Redis 只读状态由 `RedisGuard.ProtectionStatus` 返回。测试：`internal/console/console_test.go`、`internal/protection/redis_guard_test.go`。

**名词层“现状 → 变化”逐项核对**：
- [x] Runtime config 用户配置面只保留 Redis 连接；`ProtectionConfig` 改为内部默认规则，`LegacyProtectionConfig` 只兼容解析旧 `[protection]`。
- [x] `RuntimeGuard` 是进程内 source of truth；服务启动由 `cmd/webot-msg/main.go` 创建默认 disabled 的 `protection.NewRuntimeGuard()`。
- [x] 保护状态结构 `protection.Status` 已包含 enabled、botID、冻结、剩余次数、主动窗口剩余时间、时间提醒剩余时间、pending reminder 等字段。
- [x] 受保护发送事务通过 `protection.BeginOperation` 绑定同一 generation，`sender.SendProtectedText` 覆盖 `ReserveNormalSend -> SendMessage -> ReleaseNormalSend/RecordReminderSend` 生命周期。

**流程图核对**：
- [x] 启动读取 Runtime config → 忽略旧 `[protection]` 开启状态 → 创建默认 disabled RuntimeGuard → 注入 App/API，有代码落点：`cmd/webot-msg/main.go`、`internal/app/app.go`。
- [x] `/protection enable` → Redis config/PING → 切换 generation → 启动已登录 bot 检查器，有代码落点：`internal/app/app.go`、`internal/protection/runtime_guard.go`。
- [x] 普通文本发送 → RuntimeGuard operation → disabled 时 no-op / enabled 时 Redis guard，有代码落点：`internal/sender/protected_text.go`、`internal/protection/runtime_guard.go`。
- [x] `/protection status` → disabled 不读 Redis / enabled 查询 active bot 状态，有代码落点：`internal/app/app.go`、`internal/protection/redis_guard.go`。
- [x] `/protection disable` → 新事务 no-op、检查器取消、旧 generation 收尾后关闭 Redis client，有代码落点和并发交错测试覆盖。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 保护开启状态不再由 Runtime config 的 `[protection].enabled` 决定；旧配置测试断言 `Protection.Enabled=false`。
- [x] 配置文件只保留 Redis 连接配置；默认模板、部署脚本模板和用户文档均移除新 `[protection]` section。
- [x] 服务启动默认 disabled；Redis 未配置或不可用不会阻止启动，只有执行 `/protection enable` 时才连接 Redis。
- [x] `/protection status` 显示 active bot 距离次数提醒和时间提醒的剩余量；`RedisGuard.ProtectionStatus` 单测覆盖正常、缺 active window、冻结状态。
- [x] API 和控制台普通文本发送共享同一个 guard；API server 和 app 都调用 `sender.SendProtectedText`。

**明确不做逐项核对**：
- [x] 未新增 HTTP 管理接口；`internal/api` 仍只有 `/messages` 和 `/typing` 动作。
- [x] 未把 enabled 状态写回 TOML、auth store 或 Redis；grep 未发现新增持久化字段。
- [x] 控制台不修改 Redis URL/password/key prefix；`EnableProtection` 只读取启动时的 `protection.EnableConfig`。
- [x] 未恢复 TOML 保护规则配置；旧规则字段只进 `LegacyProtectionConfig`，最终规则来自代码默认值。
- [x] 未改变 `/bots/{botID}/messages`、`/bots/{botID}/typing` 的路径、鉴权方式或请求结构。
- [x] status、日志和错误路径不输出 Redis password、BotToken、APIToken、ContextToken 或完整消息正文；Redis URL parse 错误测试覆盖脱敏。

**关键决策落地**：
- [x] `RuntimeGuard` 实现 `protection.Guard`，并新增 `BeginOperation` generation 语义，解决 disable 打断多步 Redis 协议的问题。
- [x] `/protection enable` 是唯一启用入口；启动时只创建 disabled RuntimeGuard，不创建 Redis client。
- [x] `/protection disable` 不删除 Redis 状态；只阻止新事务并取消检查器，旧 generation 用 refcount 延迟关闭 client。
- [x] 旧 `[protection]` 兼容解析并忽略，同时通过启动 warning 和 Linux upgrade 提示迁移到 `/protection enable`。
- [x] Redis status 查询由保护模块实现，console 只解析命令和展示输出。

**编排层“现状 → 变化”逐项核对**：
- [x] `main` 不再通过旧 `buildProtectionGuard` 启动期启用 Redis，而是创建 `NewRuntimeGuard` 并把 Redis 配置注入 app。
- [x] `App.EnableProtection` 成功后为已有 bot 启动检查器；登录或启动监听后按当前 enabled 状态启动检查器。
- [x] `App.DisableProtection` 取消所有保护检查器并禁用 RuntimeGuard。
- [x] 时间窗口检查器在 `checkProtectionTimeWindowOnce` 内持有同一个 operation，`CheckTimeWindow` 和提醒记录不会被 disable 切成不同 guard。

**流程级约束核对**：
- [x] enable 失败不改变当前状态；`RuntimeGuard.Enable` 先创建并 PING 新 client，成功后才替换 generation，单测覆盖空 Redis URL 不改变 disabled 状态。
- [x] 重复 enable/disable 不创建重复检查器；`runningProtectionCheckers` 按 botID 去重，生命周期测试覆盖。
- [x] enabled 状态、generation 和 Redis client 替换受锁保护；race 子集测试通过。
- [x] disable 与发送交错时 release/record 仍使用原 generation；sender 和 RuntimeGuard 测试直接覆盖。
- [x] unknown `/protection xxx` 输出 usage；`/protectionfoo` 不被宽前缀吞掉，恢复普通文本路径。

**挂载点反向核对（可卸载性）**：
- [x] Runtime config 用户配置面：`internal/runtimeconfig/config.go`、部署脚本模板、用户文档。
- [x] 控制台命令：`internal/console/console.go` 和 `Controller` 接口。
- [x] 运行期 guard 注入点：`cmd/webot-msg/main.go`、`internal/app/app.go`、`internal/api/server.go`。
- [x] 保护检查器生命周期：`internal/app/app.go`。
- [x] Redis 只读状态查询：`internal/protection/redis_guard.go`。
- [x] 反向 grep：`rg "/protection|RuntimeGuard|BeginOperation|ProtectionStatus|LegacyProtection"` 命中均落在清单挂载点、测试、文档内，未发现清单外业务入口。
- [x] 拔除沙盘推演：若移除本 feature，需要撤回 console `/protection` 分支、RuntimeGuard、operation generation、app enable/disable/status、Redis status、旧配置 warning 和相关文档；无隐藏 HTTP 管理入口或 auth store 字段残留。

## 3. 验收场景核对

- [x] **S1**：新默认配置、部署脚本默认配置和用户文档不包含 `[protection]`，只保留 `[redis]`。
  - 证据来源：`rg "\\[protection\\]" docs/user scripts` 只命中旧配置迁移说明；模板正文已移除。
  - 结果：通过。
- [x] **S2**：旧配置含 `[protection] enabled=true` 仍启动解析，但保护默认 disabled 且不会启动期连接 Redis。
  - 证据来源：`TestLoadFileAcceptsLegacyProtectionSectionWithoutEnabling`、`main` 默认创建 disabled RuntimeGuard。
  - 结果：通过。
- [x] **S3**：旧 `[protection]` 被忽略时有迁移提示。
  - 证据来源：`legacyProtectionWarning` 测试；`scripts/linux-service.sh upgrade` 检测旧 section 并提示。
  - 结果：通过。
- [x] **S4**：Redis 未配置、URL 格式非法、认证/连接失败时 `/protection enable` 失败且不半开启。
  - 证据来源：`NewRedisClient` 校验、`RuntimeGuard.Enable` 先 PING 后切换、`TestRuntimeGuardEnableFailsWithoutChangingState`。
  - 结果：通过。
- [x] **S5**：Redis 可用时 enable 成功，API 和控制台普通文本发送开始经过同一个 guard。
  - 证据来源：`App.EnableProtection` 注入同一 RuntimeGuard；API/app 发送入口均为 `sender.SendProtectedText`。
  - 结果：通过。
- [x] **S6**：`/protection status` 在正常 active window 显示剩余次数和时间。
  - 证据来源：`TestRedisGuardProtectionStatus` 验证 `MessagesBeforeReminder`、`ActiveWindowRemaining`、`TimeBeforeWarning`。
  - 结果：通过。
- [x] **S7**：active key 缺失时 status 提示主动对话窗口未准备。
  - 证据来源：`TestRedisGuardProtectionStatusMissingActiveWindow`；app 输出 `Active window: not ready...`。
  - 结果：通过。
- [x] **S8**：frozen=count 时 status 显示冻结和原因。
  - 证据来源：`TestRedisGuardProtectionStatusFrozen`；app 输出 `Frozen: yes (reason)`。
  - 结果：通过。
- [x] **S9**：Redis 临时不可用时 status 输出不可用且不泄露敏感配置。
  - 证据来源：`PrintProtectionStatus` 只打印错误和非敏感字段；Redis URL/password 不进入 status 输出。
  - 结果：通过。
- [x] **S10**：disable 后新普通文本发送不再经过 Redis guard，后台检查器停止。
  - 证据来源：`TestRuntimeGuardEnableAndDisable`、`TestProtectionCheckerLifecycle`。
  - 结果：通过。
- [x] **S11**：disable 不能污染正在进行的发送事务。
  - 证据来源：`TestRuntimeGuardOperationKeepsGenerationAfterDisable`、两个 sender 交错测试。
  - 结果：通过。
- [x] **S12**：未知 `/protection xxx` 输出 usage，`/protectionfoo` 不被吞。
  - 证据来源：`TestRunHandlesProtectionCommands`、`TestRunTreatsProtectionPrefixWithoutSeparatorAsText`。
  - 结果：通过。
- [x] **S13**：HTTP `/bots/{botID}/typing` 不计入保护状态，也不受冻结影响。
  - 证据来源：`internal/api/server.go` 的 `typing` 分支直接调用 `handleTyping`，不经过 `SendProtectedText`。
  - 结果：通过。

验证命令：

```bash
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./internal/protection ./internal/sender ./internal/app
git diff --check
```

## 4. 术语一致性

- 运行期保护开关：代码使用 `RuntimeGuard`、`EnableProtection`、`DisableProtection` 表达；未再使用 TOML enabled 作为运行源。
- Redis 配置：代码使用 `RedisConfig` / `EnableConfig.RedisURL` / `RedisPassword` / `KeyPrefix`；console 不直接改这些字段。
- 保护状态：代码使用 `protection.Status` 和 `ProtectionStatus`；console/app 只做展示。
- 保护控制台命令：命令统一为 `/protection enable|disable|status`；未新增全局 `/status`。
- 受保护发送事务：代码使用 `Operation` / `BeginOperation` / guard generation；架构文档已同步该术语。
- 防冲突：`rg "HasPrefix\\(text, \"/protection\"\\)"` 无宽前缀误匹配；`/protectionfoo` 测试覆盖。

## 5. 架构归并

对照方案第 4 节：

- [x] `.codestable/architecture/ARCHITECTURE.md`：Runtime config 已归并为“只保存 Redis 连接，不保存保护开启状态；旧 `[protection]` 兼容解析但不驱动行为”。
- [x] `.codestable/architecture/ARCHITECTURE.md`：app 已归并运行期保护控制器、enable/disable/status、检查器生命周期。
- [x] `.codestable/architecture/ARCHITECTURE.md`：protection 已归并 `RuntimeGuard` generation/refcount 语义和 Redis status 查询。
- [x] `.codestable/architecture/ARCHITECTURE.md`：约束中补充 `/protection enable` Redis 连接时机、status active bot 限制、typing 不计入保护、受保护发送事务不能被 disable 切断。
- [x] `.codestable/attention.md`：本 feature 未暴露新的每个 feature 都会踩的环境/工具/工作流事项，暂不更新。

归并判据：只读 architecture 已能看出运行期保护开关、Redis 配置归属、guard generation 生命周期和 HTTP/console 共享发送保护入口。

## 6. requirement 回写

- [x] 方案 frontmatter `requirement: bot-message-bridge`。
- [x] `.codestable/requirements/bot-message-bridge.md` 已更新当前用户故事、解决方案、边界和变更记录：保护开启状态从 Runtime config 移到运行中控制台命令，TOML 只保留 Redis 配置，新增 `/protection enable|disable|status`。
- [x] `.codestable/requirements/VISION.md` 仍指向 current 的 `bot-message-bridge`，pitch 包含可选发送保护和 TOML 管理能力，无需调整。

结论：requirement 已回写，无需新增 req 或状态迁移。

## 7. roadmap 回写

- [x] 方案 frontmatter 没有 `roadmap` / `roadmap_item` 字段。

结论：非 roadmap 起头，跳过 roadmap 回写。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 `.codestable/attention.md` 的新增环境、命令或路径陷阱。

说明：`scripts/linux-service.sh` 与部署文档同步检查已经是现有 attention 条目；本次遵守并更新了脚本和用户文档。

## 9. 遗留

- 后续优化点：可在后续重构中合并 `runtimeconfig.validateRedisURL` 与 `protection.ValidateRedisURL` 的重复语义，降低校验分叉风险。
- 已知限制：保护开关仍是进程内状态，服务重启后默认关闭，需要用户重新执行 `/protection enable`；这是本 feature 的明确设计。
- 已知限制：保护模式仍按 bot 计数，不支持同一 bot 多会话独立计数。
- 实现阶段顺手发现：`App.protectionEnabled` 仍作为非 RuntimeGuard 测试/兼容路径存在，主路径 source of truth 是 `RuntimeGuard`；后续若清理 App 状态镜像，可走 refactor 流程。
