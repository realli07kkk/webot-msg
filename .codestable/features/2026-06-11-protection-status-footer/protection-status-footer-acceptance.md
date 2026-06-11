# protection-status-footer 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-11
> 关联方案 doc：`.codestable/features/2026-06-11-protection-status-footer/protection-status-footer-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**：

- [x] `internal/sender` 状态行拼装：`hello` + 预留快照（剩余 4 条，9h30m）→ 实际输出 `hello\n[限流阈值] 剩余可发 4 条 | 距离限制还有 9h30m`。证据：`TestProtectionStatusFooter`、`TestSendProtectedTextAppendsStatusFooterWhenSnapshotExists`、`TestHandleSendMessageAppendsStatusFooterWithoutChangingResponse`。
- [x] 第 9 条触发 `send_then_reminder`：普通消息状态行显示剩余 0 条，随后提醒消息不追加状态行。证据：`TestSendProtectedTextDoesNotAppendStatusFooterToReminder`、`TestSendProtectedTextReminderRecordUsesReservedGenerationAfterDisable`。
- [x] `NoopGuard.ReserveNormalSend`：保护关闭路径 `HasStatus=false`，发送正文逐字节保持 `hello`。证据：`TestNoopGuardReserveNormalSendHasNoStatusSnapshot`、`TestSendProtectedTextLeavesTextUnchangedWithoutStatusSnapshot`。

**名词层“现状 → 变化”逐项核对**：

- [x] `Reservation` 增加 `HasStatus` / `MessagesBeforeReminder` / `TimeBeforeWarning`，字段命名与 `protection.Status` 对齐。证据：`internal/protection/guard.go`。
- [x] reserve Lua 脚本在 `send` / `send_then_reminder` 分支返回 `{kind, reason, out_count, pttl_ms}`；`reject` / `reminder_only` 仍返回两元组。证据：`internal/protection/redis_guard.go`、`TestRedisGuardReserveNormalSendReturnsStatusSnapshot`、`TestRedisGuardReserveNormalSendRejectsFrozenAndMissingActive`、`TestRedisGuardReserveNormalSendTimeWarningSendsReminderOnly`。
- [x] 状态行拼装函数位于 `internal/sender/status_footer.go`，`HasStatus=false` 返回空串。证据：`TestProtectionStatusFooter`。

**流程图核对**：

- [x] `ReserveNormalSend` → 按 reservation 分支 → `protectionStatusFooter` 拼装 → `SendMessage` → 失败 release / 成功后可发送提醒，代码均有落点。证据：`internal/sender/protected_text.go`，grep 命中 `protectionStatusFooter` 只在普通发送分支。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 保护模式开启时，HTTP API 普通文本追加状态行后发往 iLink。证据：`TestHandleSendMessageAppendsStatusFooterWithoutChangingResponse`。
- [x] 控制台普通文本走同一发送编排，同样追加状态行。证据：`TestSendTextAppendsStatusFooter`。
- [x] 保护关闭时内容原样发送。证据：`TestSendProtectedTextLeavesTextUnchangedWithoutStatusSnapshot`。
- [x] 用户 review 提出的 opt-in 兼容性变更未纳入；用户已明确要求保持原始 SDD 设计并忽略该问题。本报告按“保护开启即追加”验收。

**明确不做逐项核对**：

- [x] 不给保护提醒消息追加状态行。证据：`TestSendProtectedTextDoesNotAppendStatusFooterToReminder`。
- [x] 不给 `/bots/{botID}/typing` 追加；typing 不调用 `ReserveNormalSend`。证据：`TestHandleTypingDoesNotReserveOrAppendStatusFooter`。
- [x] 不新增 TOML 配置 key；`runtimeconfig` 未改，grep 确认无新增配置项。
- [x] 不修改 HTTP API 请求 / 响应 JSON 契约；响应不回显追加后的正文。证据：`TestHandleSendMessageAppendsStatusFooterWithoutChangingResponse`。
- [x] 不改变 Redis key 结构和既有 Lua 判断语义，仅扩展成功分支返回值。证据：`internal/protection/redis_guard.go` diff 与 Redis guard 现有测试。
- [x] 不在预留之后用 `ProtectionStatus` 二次查询拼装状态行。证据：grep 确认 `internal/sender` 中无 `ProtectionStatus` 调用。

**关键决策落地**：

- [x] 快照随 reserve Lua 原子返回，不做二次查询。证据：`runReservationScript` 直接解析 Lua 返回的 `out_count` 与 `pttl_ms`。
- [x] 拼装与追加放在 `internal/sender`，API 与控制台共享。证据：`api.Server.handleSendMessage` 和 `App.SendText` 都调用 `sender.SendProtectedText`。
- [x] 时间语义为 active TTL 减去 `time_warning_before`。证据：`reservationWithStatus` 中 `pttl - timeWarningBefore`，`TestRedisGuardReserveNormalSendReturnsStatusSnapshot` 覆盖 23h30m。
- [x] 保护关闭路径靠快照标志位识别。证据：`NoopGuard` 返回无快照，`protectionStatusFooter` 对 `HasStatus=false` 返回空串。

**编排层“现状 → 变化”逐项核对**：

- [x] 在预留成功与 `SendMessage` 之间插入纯计算追加步骤。证据：`sendProtectedText` 中普通发送分支先构造 `messageText`。
- [x] `reminder_only`、提醒发送、release 路径保持原语义。证据：原有 sender / app 测试继续通过，新增提醒不追加测试通过。

**流程级约束核对**：

- [x] 拼装无新增错误路径；快照缺失静默不追加。证据：`protectionStatusFooter` 无 error 返回，`HasStatus=false` 返回空串。
- [x] 并发快照来自同一次 Lua 调用。证据：`TestRedisGuardReserveNormalSendConcurrentStatusSnapshots`。
- [x] 状态行反映本次发送计入后剩余值；发送失败 release 后重试重新预留。证据：`TestRedisGuardReserveNormalSendReturnsStatusSnapshot`、`TestSendProtectedTextReleaseUsesReservedGenerationAfterDisable`。
- [x] 日志不打印拼装后完整正文。证据：grep 状态行拼装路径未新增日志。
- [x] 保护关闭路径发送正文逐字节不变；HTTP 请求 / 响应 JSON 零变化。证据：sender / API 单测。

**挂载点反向核对**：

- [x] `protection.Reservation` 快照字段与 reserve Lua 返回值扩展落在 `internal/protection/guard.go`、`internal/protection/redis_guard.go`。
- [x] 普通文本发送前追加步骤落在 `internal/sender/protected_text.go`。
- [x] 状态行文案格式与拼装函数落在 `internal/sender/status_footer.go`。
- [x] 反向 grep：`HasStatus`、`MessagesBeforeReminder`、`TimeBeforeWarning`、`protectionStatusFooter` 的业务代码命中均落在上述挂载点；测试命中用于覆盖验收场景。
- [x] 拔除沙盘推演：移除 `Reservation` 快照字段 + reserve 脚本扩展会让状态行无数据源；移除 sender 追加步骤会让微信侧状态行消失；移除 `status_footer.go` 会让文案格式消失。无清单外残留。

## 3. 验收场景核对

- [x] **S1**：保护关闭（默认），HTTP API / 控制台发送 `hello` → iLink 收到 `hello`，无状态行。
  - 证据来源：单测 `TestSendProtectedTextLeavesTextUnchangedWithoutStatusSnapshot`
  - 结果：通过
- [x] **S2**：保护开启第 5 条普通文本 `hello`（TTL = 10h，warn = 30m）→ iLink 收到 `hello\n[限流阈值] 剩余可发 4 条 | 距离限制还有 9h30m`；HTTP 响应结构不变。
  - 证据来源：单测 `TestHandleSendMessageAppendsStatusFooterWithoutChangingResponse`
  - 结果：通过
- [x] **S3**：保护开启，控制台发送普通文本 → 同样追加状态行。
  - 证据来源：单测 `TestSendTextAppendsStatusFooter`
  - 结果：通过
- [x] **S4**：第 9 条触发 `send_then_reminder` → 状态行显示剩余 0 条；随后提醒消息不含状态行。
  - 证据来源：单测 `TestSendProtectedTextDoesNotAppendStatusFooterToReminder`
  - 结果：通过
- [x] **S5**：冻结状态下发送 → HTTP 429 / 控制台锁定错误，不调用 iLink。
  - 证据来源：单测 `TestHandleSendMessageRejectsFrozenBeforeSendingUserText`、`TestSendTextRejectsFrozenBeforeSendingUserText`
  - 结果：通过
- [x] **S6**：iLink 普通文本发送失败 → 预留回退；下次发送重新预留并按新快照拼装。
  - 证据来源：单测 `TestSendProtectedTextReleaseUsesReservedGenerationAfterDisable`、Redis release 既有测试
  - 结果：通过
- [x] **S7**：TTL − warn 不足 1 分钟但大于 0 → 状态行时间显示 `<1m`。
  - 证据来源：单测 `TestProtectionStatusFooter`
  - 结果：通过
- [x] **S8**：两条普通文本并发发送 → 各自剩余条数来自各自预留快照，互不相同且与最终计数一致。
  - 证据来源：单测 `TestRedisGuardReserveNormalSendConcurrentStatusSnapshots`
  - 结果：通过
- [x] **S9**：调用 `/bots/{botID}/typing` → 行为与现状一致，无任何追加。
  - 证据来源：单测 `TestHandleTypingDoesNotReserveOrAppendStatusFooter`
  - 结果：通过

无前端改动，不需要浏览器验证。

## 4. 术语一致性

- 保护状态行 / status footer：代码使用 `protectionStatusFooter`，命中集中在 `internal/sender/status_footer.go`、`internal/sender/protected_text.go` 和测试，语义一致。
- 预留快照 / reservation snapshot：代码用 `Reservation.HasStatus`、`MessagesBeforeReminder`、`TimeBeforeWarning` 表达，命中集中在 `internal/protection`、`internal/sender` 和测试，语义一致。
- 距离限制触发剩余时间：代码字段沿用 `TimeBeforeWarning`，计算为 active TTL 减 `timeWarningBefore`，语义一致。
- 防冲突：`internal/sender` 中无 `ProtectionStatus` 二次查询；`runtimeconfig`、`internal/ilink` 未新增 footer 相关概念。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md` 术语“保护模式”已补充：开启时普通文本末尾追加保护状态行。
- [x] `.codestable/architecture/ARCHITECTURE.md` 新增“预留快照”术语：说明快照来自同一次 Redis Lua reserve。
- [x] `.codestable/architecture/ARCHITECTURE.md` `internal/sender` 段已补充：发送编排在预留成功后按快照拼装状态行，保护关闭 / 快照缺失时原文发送。
- [x] `.codestable/architecture/ARCHITECTURE.md` `internal/protection` 段已补充：普通发送预留成功时 Lua 返回剩余额度和 TTL 快照。
- [x] `.codestable/architecture/ARCHITECTURE.md` 已知约束已补充：状态行只在保护开启且普通文本预留成功时追加，数据来自同一次 Lua reserve，HTTP JSON 契约不变，typing 和提醒不追加，sender 禁止二次查询。

## 6. requirement 回写

- [x] frontmatter `requirement: bot-message-bridge` 指向 current req。
- [x] `.codestable/requirements/bot-message-bridge.md` 已更新用户故事：保护模式下用户在微信侧每条普通消息可见剩余额度与剩余时间。
- [x] `.codestable/requirements/bot-message-bridge.md` 已更新“怎么解决”：保护开启且普通文本预留成功时追加状态行，数据来自同一次 Redis 预留。
- [x] `.codestable/requirements/bot-message-bridge.md` 已更新边界：HTTP API 和控制台普通文本真实正文会追加状态行，HTTP JSON 不新增字段，提醒和 typing 不追加。
- [x] `.codestable/requirements/bot-message-bridge.md` 已追加 2026-06-11 变更记录。

## 7. roadmap 回写

- [x] 方案 frontmatter 无 `roadmap` / `roadmap_item` 字段，本 feature 非 roadmap 起头，跳过 roadmap 回写。

## 8. attention.md 候选盘点

- [x] 候选 1：`.codestable/tools/validate-yaml.py` 当前没有执行权限，需用 `python3 .codestable/tools/validate-yaml.py --file <path>` 调用。建议由用户确认后用 `cs-note` 追加到 `.codestable/attention.md` 的“命令与脚本陷阱”。

## 9. 遗留

- 后续优化点：无。
- 已知限制：保护模式开启时普通文本真实发送正文会追加状态行，这是本 feature 的设计语义；用户已在验收前明确要求保持原始 SDD 设计并忽略 opt-in 兼容性 finding。
- 实现阶段“顺手发现”列表：无。

## 验证记录

- `go test ./...`：通过
- `go vet ./...`：通过
- `python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-06-11-protection-status-footer/protection-status-footer-checklist.yaml`：通过
