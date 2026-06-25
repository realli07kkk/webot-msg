---
doc_type: feature-acceptance
feature: 2026-06-25-protection-send-queue
status: accepted
summary: 验收保护冻结期 API 发送队列，Redis 原子 ingress、FIFO 重放、disable 生命周期、文档与需求回写已完成
tags: [protection, redis, queue, drain, api, fifo]
accepted_at: 2026-06-25
---

# protection-send-queue 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-25
> 关联方案 doc：`.codestable/features/2026-06-25-protection-send-queue/protection-send-queue-design.md`

验证基线：`go test -count=1 ./...`、`go vet ./...`、`git diff --check`、`gofmt -l ...`、`python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-06-25-protection-send-queue/protection-send-queue-checklist.yaml` 均已通过。本报告按 design 第 0/1/2/3/4 节逐项验收；代码偏差已在实现阶段修复，未留下已知未处理偏差。

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `queuedPayload{Text, EnqueuedMs}` JSON 结构落地于 `internal/protection/queue.go:12`，Lua 只接收序列化字符串，TTL 判定在 Go 端执行；测试 `TestRedisGuardQueuePeekDropFIFOAndTTL` 覆盖解码、顺序和 PEXPIRE。
- [x] `SendQueueController`、`IngressOutcome`、`Ingress{Outcome, Reservation, QueueLen, SendReminder, Reason}` 落地于 `internal/protection/queue.go:17`；`RedisGuard` 编译期断言实现该接口。
- [x] 发送队列 key 为 `{prefix}:protect:{botID}:queue`，沿用 `{botID}` hash tag，与 `:state` / `:active` 同 slot；代码 `internal/protection/queue.go:98`，测试通过 miniredis 操作队列。
- [x] `AcquireOrEnqueue` 单 Lua 返回 `send` / `send_then_reminder` / `queued` / `queued_reminder` / `full` 并映射为 `IngressSendNow` / `IngressQueued` / `IngressQueueFull`；代码 `internal/protection/queue.go:40`、`:158`，测试覆盖所有主要分支。
- [x] `protection.Status.QueuedCount` 与 `RedisGuard.Status` 的 `LLEN(:queue)` 落地；代码 `internal/protection/guard.go:90`、`internal/protection/redis_guard.go:149`，测试 `TestRedisGuardQueueLenAndProtectionStatusQueuedCount`。
- [x] `RedisGuardConfig` / `EnableConfig` / `runtimeconfig.ProtectionConfig` 增加 `QueueMaxLen`、`QueueTTL` 内部字段，默认 1000 / active window，不暴露 TOML key；代码 `internal/runtimeconfig/config.go:31`、`:173`、`:298`、`:309`，`cmd/webot-msg/main.go:93` 透传。
- [x] `sender` 抽出 `sendWithReservation`，新增 `SendOrEnqueueText` 和 `Outcome`；`SendProtectedTextWithOptions` 仍是 plain reserve 发送入口。代码 `internal/sender/protected_text.go:54`、`:61`、`:145`。

**名词层“现状 → 变化”逐项核对**：
- [x] API ingress 从直接调用 protected send 改为 `SendOrEnqueueText` 三分支；代码 `internal/api/server.go:143`、`:156`。
- [x] 控制台和 drainer 仍复用 `SendProtectedTextWithOptions` plain reserve，不走队列感知 ingress；代码 `internal/app/app.go:476`、`internal/app/send_queue.go:135`。
- [x] `RuntimeGuard` operation 暴露 `SendQueueController`，disable 后已开始 operation 仍绑定原 generation；代码 `internal/protection/runtime_guard.go:224`，测试 `TestRuntimeGuardOperationExposesSendQueueController`。

**流程图核对**：
- [x] API 图节点均有落点：`POST /messages` → `SendOrEnqueueText` → `AcquireOrEnqueue` → sent/queued/full；queued-reminder 只发送提醒，不发送用户原文。
- [x] drain 图节点均有落点：`PeekQueued` → TTL 丢弃 / 上下文检查 / `SendProtectedTextWithOptions` → `DropFront`；再冻结、网络失败、无上下文均停止并保留队首。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 保护开启 + 冻结时 API 返回 `202`，body 含 `status:"queued"` 和 `queued` 长度，且不调用 iLink；证据：`TestHandleSendMessageQueuesFrozenWithoutSending`。
- [x] 冻结期连续入队保持到达顺序；证据：`TestRedisGuardQueuePeekDropFIFOAndTTL`、`TestDrainSendQueueReplaysFIFOAndClears`。
- [x] 恢复 / enable / auto-restore 触发 drainer，按 FIFO 投递并清空；代码 `internal/app/app.go:257`、`:315`、`:760`，证据：app drainer 测试覆盖 FIFO，状态恢复路径已挂载。
- [x] 队列未清空时新 API 请求继续入队、不插队；证据：`TestRedisGuardAcquireOrEnqueueQueuesWhenBacklogExists`。
- [x] 队列满返回 `503`，已有队列不丢；证据：`TestRedisGuardAcquireOrEnqueueReturnsQueueFull`、`TestHandleSendMessageReturnsServiceUnavailableWhenQueueFull`。
- [x] 单条 TTL 过期在重放时丢弃，不投递；证据：`TestDrainSendQueueDropsExpiredPayload`。

**明确不做逐项核对**：
- [x] 控制台 `/send` 不入队，冻结时仍即时返回 protection lock；代码 `internal/app/app.go:476`，证据：`TestSendTextRejectsFrozenBeforeSendingUserText`。
- [x] typing 不入队、不计数；代码 `internal/api/server.go:174`，证据：`TestHandleTypingDoesNotReserveOrAppendStatusFooter`。
- [x] 保护提醒不入队，仍直接发提醒并记录计数；代码 `internal/sender/protected_text.go:202`。
- [x] 网络 / iLink 失败不 RPUSH，仍 release 预留并返回错误；代码 `internal/sender/protected_text.go:168`，证据：`TestSendTextReleasesReservationWhenSendFails`、`TestDrainSendQueueReleasesReservationAfterSendContextCanceled`。
- [x] 不新增 TOML queue key；反向 grep `queue` 于 TOML 解析字段无新增 tag，队列字段均为 `toml:"-"` 内部 resolved config。
- [x] `protection.Guard` 核心接口未新增队列方法，队列只在 `SendQueueController` 断言接口上。

**关键决策落地**：
- [x] 冻结语义只对 API 从拒绝改为入队；控制台仍 fail closed。API 文档已补旧客户端 `429 -> 202 status=queued` 迁移说明。
- [x] ingress 原子性由一个 Redis Lua 脚本完成，含 `frozen/LLEN`、reserve、RPUSH、PEXPIRE；代码 `internal/protection/queue.go:158`。
- [x] FIFO + 不插队通过 `LLEN>0` 入队纪律和 `LINDEX -> 发送 -> LPOP` 达成，drainer 不用 pop-before-send。
- [x] at-least-once 明确接受，不新增去重状态；架构和 requirement 已写入该限制。
- [x] disable 生命周期已收紧：`/protection disable` 停止 active drainer，不 flush Redis 队列；发送成功后的 pop 使用独立 bounded commit ctx，发送失败后的 release/提醒 record 也使用独立 bounded commit ctx。证据：`TestDisableProtectionStopsActiveSendQueueDrainerAndKeepsRemaining`、`TestDrainSendQueuePopsSentMessageAfterDrainContextCanceled`、`TestDrainSendQueueReleasesReservationAfterSendContextCanceled`。

**编排层“现状 → 变化”核对**：
- [x] `internal/api` 挂入 queued/full 响应，保护关闭和未冻结仍 `200`。
- [x] `internal/app` 新增 per-bot 单 drainer，挂载到 enable、auto-restore、`RecordActiveConversation` 成功后和 delete/disable 停止路径。
- [x] `internal/protection` 新增队列 Lua、peek/pop/llen；`recordActiveConversationScript` 和 `releaseNormalSendScript` 不动 `:queue`。
- [x] `internal/sender` 保持控制台发送语义，API 专用 `SendOrEnqueueText` 只在 `SendQueueController` 存在时介入。

**流程级约束核对**：
- [x] 错误语义：API 直发失败 release + 500，不入队；drain 失败停止并保留队首；再冻结停止并保留队首。
- [x] 有界：`LLEN < QueueMaxLen` 才 RPUSH，RPUSH 刷新 PEXPIRE，drain 精确检查 `enqueued_ms`。
- [x] 去重 drain：同一 bot 至多一个 drainer，并发触发只置 `rerun`；退出窗口内新触发可继续拉起。代码 `internal/app/send_queue.go:20`、`:42`。
- [x] 持久化语义：队列只在 Redis，不进 auth store / protection 状态文件；disable 不 flush。

**挂载点反向核对（可卸载性）**：
- [x] M1 `handleSendMessage` 改调 `SendOrEnqueueText` + `202` / `503` 分支。
- [x] M2 per-bot drainer 子系统 + `RecordActiveConversation` / enable / restore 触发。
- [x] M3 Redis `:queue` key + ingress Lua / peek / pop / llen 位于 `internal/protection`。
- [x] M4 `/protection status` 增 `Queued messages: N`。
- [x] 反向 grep：`AcquireOrEnqueue` / `PeekQueued` / `DropFront` / `QueueLen` / `startSendQueueDrainer` / `SendOrEnqueueText` / `Queued messages` 的非测试落点全部对应 M1-M4。
- [x] 拔除沙盘推演：移除 M1-M4 后 API 回到旧 protected send，app 无 drainer，Redis 无 `:queue`，status 无队列行；没有清单外残留挂载点。

## 3. 验收场景核对

对照方案第 3 节关键场景清单：

- [x] **保护关闭直发**：API 发送 `200` 正常投递，无队列参与。证据：`TestSendOrEnqueueTextFallsBackToProtectedSend`、`TestHandleSendMessageAppendsStatusFooterWithoutChangingResponse`。
- [x] **保护开启未冻结直发**：队列空时 `AcquireOrEnqueue` 返回 `IngressSendNow`，API `200` 投递并计数。证据：`TestRedisGuardAcquireOrEnqueueSendsNowWhenQueueEmpty`、`TestHandleSendMessageWithQueueControllerSendsNow`。
- [x] **冻结入队**：API `202 status=queued`，Redis queue 多一条，不调用 iLink。证据：`TestRedisGuardAcquireOrEnqueueQueuesWhenFrozen`、`TestHandleSendMessageQueuesFrozenWithoutSending`。
- [x] **冻结期顺序**：连发多条后 FIFO 顺序与到达一致。证据：`TestRedisGuardQueuePeekDropFIFOAndTTL`。
- [x] **恢复重放 FIFO**：drainer 按队首顺序发送并清空，每条走普通发送漏斗，生成 ID/审计按投递时刻执行。证据：`TestDrainSendQueueReplaysFIFOAndClears`、sender ID/audit 既有测试。
- [x] **堆积清空前不插队**：队列非空时 ingress 一律入队。证据：`TestRedisGuardAcquireOrEnqueueQueuesWhenBacklogExists`。
- [x] **重放途中再触限即停**：RejectionError 停止 drain，队首保留。证据：`TestDrainSendQueueStopsOnRejectionAndKeepsFront`。
- [x] **队列上限**：满队列新请求 `503`，已有 FIFO 不丢。证据：`TestRedisGuardAcquireOrEnqueueReturnsQueueFull`、`TestHandleSendMessageReturnsServiceUnavailableWhenQueueFull`。
- [x] **TTL 丢弃**：过期 payload 被 DropFront 丢弃且不投递；PEXPIRE 由 ingress 设置。证据：`TestDrainSendQueueDropsExpiredPayload`、`TestRedisGuardQueuePeekDropFIFOAndTTL`。
- [x] **网络失败不入队**：直发失败 release，drain 失败保留队首并停止。证据：`TestSendTextReleasesReservationWhenSendFails`、`TestDrainSendQueueReleasesReservationAfterSendContextCanceled`。
- [x] **auto-restore drain**：`restoreProtectionState` 成功 enable 后遍历 bot 启动 drainer；代码 `internal/app/app.go:313`、`:315`，与 drainer FIFO 测试共同覆盖行为。
- [x] **`/protection status` 展示队列长度**：证据：`TestPrintProtectionStatusShowsQueuedMessages`。
- [x] **send_then_reminder 边界**：触发次数阈值最后一条仍直发 `200`，下一条才入队。证据：`TestRedisGuardAcquireOrEnqueueReturnsSendThenReminderAtCountThreshold`。

非前端改动，无需浏览器验证。

## 4. 术语一致性

对照方案第 0 节 + 第 2.1 节命名 grep 代码：

- 发送队列 / send queue：代码实体集中在 `internal/protection/queue.go`、`internal/app/send_queue.go`、`SendQueueController`，命名一致。
- 入队 / enqueue：API 入口统一使用 `AcquireOrEnqueue`，未在控制台路径出现入队调用。
- 重放 / drain：app 层统一使用 `startSendQueueDrainer` / `drainSendQueue`，无旧 `replay` 混用。
- 队列负载：仅 `queuedPayload{Text, EnqueuedMs}`，未缓存 `ContextToken`。
- 队列上限 / TTL：`QueueMaxLen` / `QueueTTL` 命名贯穿 runtimeconfig、EnableConfig、RedisGuardConfig。
- 防冲突：新增 Redis 段为 `:queue`，与既有 `:state` / `:active` / `audit:*` 不重叠。

## 5. 架构归并

对照 design 第 4 节，已实际写入 `.codestable/architecture/ARCHITECTURE.md`：

- [x] §0 术语：新增/修订保护模式、冻结状态、发送队列、重放/drain、队列上限/TTL。
- [x] §2 结构与交互：`internal/app` 增 drainer 生命周期，`internal/sender` 增 `SendOrEnqueueText` 和 bounded commit context，`internal/protection` 增 `SendQueueController` / 队列 Lua，`internal/api` 增 `202` / `503` 语义。
- [x] §3 数据与状态：新增 `{prefix}:protect:{botID}:queue` List schema、Redis source of truth、不进 auth store / state file、payload 不存 context token。
- [x] §4 关键决策：修订旧“冻结期 API 429”为“API 入队 202、控制台拒绝”，新增单 Lua ingress、peek-send-pop、at-least-once、有界队列、disable 不 flush、断言式扩展。
- [x] §5 代码锚点：新增 `SendOrEnqueueText`、`SendQueueController`、`drainSendQueue`。
- [x] §6 已知约束：新增 API-only 入队、网络失败不入队、at-least-once、不新增 TOML、disable 保留队列。
- [x] §7 相关文档：`docs/user/runtime-config.md` 描述更新为含发送保护队列。

## 6. requirement 回写

`requirement: bot-message-bridge` 指向 current req，本次改变用户视角和 API 行为，已按 `cs-req update` 回写：

- [x] `.codestable/requirements/bot-message-bridge.md` frontmatter `last_reviewed` 更新到 2026-06-25，pitch/tags 增加冻结期 API 补发队列。
- [x] 用户故事新增“自动化流程希望冻结期 API 文本先被接收并恢复后按序补发”。
- [x] “为什么需要 / 怎么解决”补充 API 等待队列、恢复后 FIFO 补发、队满响应、控制台即时拒绝。
- [x] “边界”补充 API-only、控制台/typing/提醒/网络失败不入队、队列上限/TTL 不进 TOML、disable 不清队列、at-least-once。
- [x] “变更记录”新增 2026-06-25 条目。
- [x] `.codestable/requirements/VISION.md` 同步 pitch 与 `last_reviewed`。

cs-req 自查：语气保持用户能力层，未把 Lua/drainer 等实现细节塞进 req；边界和迁移影响有明确描述。

## 7. roadmap 回写

design frontmatter 无 `roadmap` / `roadmap_item` 字段 → 非 roadmap 起头，跳过 items.yaml 和 roadmap 主文档回写。

## 8. attention.md 候选盘点

本 feature 未暴露需要补入 `.codestable/attention.md` 的新硬约束。已有注意事项中的 YAML 校验命令仍适用；本次按 `python3 .codestable/tools/validate-yaml.py --file ...` 执行。

## 9. 遗留

- 已知限制：发送队列按 bot 而不是按会话隔离，重放使用当前 bot 上下文；这是 design 明确约束。
- 已知限制：队列重放为 at-least-once，不承诺 exactly-once；接收方需要容忍少数重复投递。
- 后续优化观察：`internal/app/app.go` 仍偏大，本 feature 已把 drainer 放到 `internal/app/send_queue.go`，如继续膨胀建议另走 `cs-refactor` 评估后台子系统拆分。
- 无新开 issue。
