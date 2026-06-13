# message-id-audit 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-13
> 关联方案 doc：`.codestable/features/2026-06-13-message-id-audit/message-id-audit-design.md`

验证基线：`go vet ./...` 干净、`go test ./...` 全 ok、`gofmt -l .` 无 diff。实现与 design **零偏差**，无需回填代码或方案。

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `audit.Auditor`（`internal/audit/audit.go:19`）：`Enabled() bool` + `Record(ctx, RecordInput) error`，`NoopAuditor` 关闭实现 → 一致
- [x] `RecordInput{ID, SentAt, Body}`（`audit.go:24`）/ `EnableConfig{RedisURL, RedisPassword, KeyPrefix, TimeTTL, BodyTTL}`（`audit.go:30`）→ 一致
- [x] `Recorder.Record`（`audit.go:118`）：关闭态返回 nil；开启用一次 `Pipelined` 写 time(UnixMilli) + body，TTL 各取配置；写失败返回 error → 一致
- [x] key 命名 `{prefix}:audit:time:{id}` / `{prefix}:audit:body:{id}`（`audit.go:172/176`）→ 一致
- [x] `audit.StateStore` + `PersistedState{AuditEnabled}`（`state_store.go:16`），原子 temp+rename、owner-only 0600/0700 → 与 `protection.StateStore` 同形
- [x] `sender.TextOptions{IDGenerator, Auditor, Now}` + `SendProtectedTextWithOptions`（`protected_text.go:37`）；`DefaultIDGenerator` = `uuid.NewV7().String()`（`protected_text.go`）→ 一致
- [x] `console.Controller` 新增 `EnableAudit/DisableAudit/PrintAuditStatus`（`console.go:29`），`App` 实现（`app.go:375/...`）→ 一致

**名词层"现状 → 变化"逐项核对**：
- [x] sender 漏斗：在最终文本组装后追加 ID、发送成功后 Record → 代码落点 `protected_text.go:94/104`
- [x] `DefaultAuditStatePath`（`runtimeconfig/config.go:24`，`toml:"-"` 不暴露 key）→ 一致

**流程图核对**（2.2 mermaid）：
- [x] 生成 ID（仅 send/send_then_reminder 分支）、ID 在 footer 之后、发送成功后按需 Record、reject/reminder_only 不生成 ID —— grep + 单测均确认

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] ID 无条件拼接：`TestSendProtectedTextAppendsIDWithoutStatusSnapshot`、`...DefaultIDGeneratorAppendsUUIDV7`（断言 uuid v7 版本位）
- [x] 审计开启写两 key + TTL：`TestRecorderRecordWritesAuditKeysWithTTL`
- [x] 重启自动恢复：`TestRestoreAuditStateEnablesRecorder`
- [x] otel span 同 trace：`TestTelemetryE2EExportsInboundOutboundAndAuditSpansWithSameTrace`
- [x] `/audit disable` 后不写、`/audit status` 展示开关+TTL：`TestAuditCommandsPersistStateAndControlRecording`、`TestPrintAuditStatusShowsSwitchAndTTL`

**明确不做逐项核对**（反向核对项，grep）：
- [x] reminder/typing 不带 ID、不审计：`sendProtectionReminder` 直发 reminderText 无 ID；typing 不走 sender。`TestSendProtectedTextDoesNotAppendStatusFooterToReminder` 断言 reminder 行 = `"reminder"`
- [x] audit 无独立 redis 配置：`internal/audit` 仅透传 `EnableConfig.RedisURL/Password` 给 `protection.NewRedisClient`，无 TOML / url 解析（grep 确认）
- [x] API 响应不含 ID 字段：`grep "id"/message_id` 于 `server.go` 无命中
- [x] 无审计内容读取命令：grep 无 get/read audit
- [x] 不 import `redisotel`/`go-redis/extra`：grep 无命中

**关键决策落地**：
- [x] D1 ID 无条件拼接、与审计解耦 → sender 在 send 分支始终生成
- [x] D2 正文兼容与迁移策略（design 后补）→ `docs/user/runtime-config.md`「消息 ID 与发送审计」节 + `linux-systemd-deploy.md` 升级说明明确告知末行 uuid v7、API 响应不变
- [x] D3 审计 fail-open / 保护 fail-closed → `Record` 出错只 `log.Printf`（`protected_text.go:104`），`TestSendProtectedTextAuditFailureDoesNotFailSend`
- [x] D4 otel 手工 child span + 仅有父 span 时创建，不引入 redisotel → `startRecordSpan` 用 `oteltrace.SpanContextFromContext(ctx).IsValid()` 门控（`audit.go:180`），`TestRecorderRecordCreatesSpanOnlyWithParent`
- [x] D5 Recorder mutex 原子换 client、不复用 generation/refcount → `audit.go:48-116`
- [x] D6 key 带 key_prefix、复用 `protection.NewRedisClient` → `audit.go:73/172`
- [x] ID 生成 fail-open → `TestSendProtectedTextIDGenerationFailureSkipsAuditAndStillSends`

**编排层"现状 → 变化"核对**：
- [x] ID 行插在保护状态行之后（最底部）：`TestSendProtectedTextAppendsStatusFooterWhenSnapshotExists` 断言 `hello\n[限流阈值]...\n{ID}`
- [x] body = 完全体（含 footer + ID）：同测试断言 `record.Body == 最终发出文本`
- [x] `app.Run` 新增 `restoreAuditState`，与 `restoreProtectionState` 并列（`app.go:131`）

**流程级约束核对**：
- [x] 持久化语义：enable/disable 落 `audit.json`；恢复只一次，Redis 不可用不改写文件 → `TestRestoreAuditStateFailureDoesNotRewriteState`
- [x] 顺序：ID 先于发送生成；`SentAt = opts.Now()`；Record 在发送成功后
- [x] span 属性不含正文/凭据：`TestTelemetrySpansOmitSensitiveRequestValues`；`startRecordSpan` 仅设 `db.system.name/db.operation.name/audit.message_id`

**挂载点反向核对（可卸载性）**——对照第 2.3 节：
- [x] M1 `/audit` 命令（`commands.go:32` + `console.go:141/158`）
- [x] M2 `[audit]` TOML（`config.go:47/86/338`）
- [x] M3 `audit.json` 状态文件（`audit/state_store.go` + `app.go` restore/persist）
- [x] M4 审计 Redis key schema（`audit.go:172/176`）
- [x] M5 sender 注入 ID 行 + Record（`protected_text.go:94/104`）
- [x] M6 部署脚本 `[audit]`（`linux-service.sh` write_default_audit_config + ensure_audit_config_section）
- [x] **反向 grep**：非测试源码中 `audit`/`messageID`/`IDGenerator`/`NewV7` 落点 = {main.go, app.go, audit/, console/{commands,console}.go, runtimeconfig/config.go, sender/protected_text.go} + 部署脚本/docs，全部落在 M1–M6 内，无清单外引用
- [x] **拔除沙盘推演**：移除 M1–M6（含 audit 包、`[audit]`/`DefaultAuditStatePath`、sender 两行注入、main/app wiring、部署段）后无残留依赖；ID 行随 sender 注入移除而消失，feature 完整卸载

## 3. 验收场景核对

对照方案第 3 节，逐条可观察证据：

- [x] **ID 总是拼接（审计关）** — 证据：`TestSendProtectedTextAppendsIDWithoutStatusSnapshot` / 单测；通过
- [x] **ID 在最底部** — `TestSendProtectedTextAppendsStatusFooterWhenSnapshotExists`；通过
- [x] **审计开启写两 key（time=UnixMilli/PTTL, body=完全体/PTTL）** — `TestRecorderRecordWritesAuditKeysWithTTL`（miniredis）；通过
- [x] **审计关闭不写** — `TestRecorderRecordClosedNoop` + `TestAuditCommandsPersistStateAndControlRecording`；通过
- [x] **持久化恢复（重启自动开启 + audit.json=true）** — `TestRestoreAuditStateEnablesRecorder`、`...PersistStateAndControlRecording`；通过
- [x] **恢复失败保持关闭、不改写文件** — `TestRestoreAuditStateFailureDoesNotRewriteState`；通过
- [x] **审计 fail-open** — `TestSendProtectedTextAuditFailureDoesNotFailSend`；通过
- [x] **ID 生成 fail-open** — `TestSendProtectedTextIDGenerationFailureSkipsAuditAndStillSends`；通过
- [x] **otel span（API 同 trace / console 无 root span）** — `TestTelemetryE2EExports...SameTrace` + `TestRecorderRecordCreatesSpanOnlyWithParent` + `TestTelemetrySpansOmitSensitiveRequestValues`；通过
- [x] **范围排除（reminder/typing/reject/reminder_only）** — `TestSendProtectedTextDoesNotAppendStatusFooterToReminder`、`TestHandleTypingDoesNotReserveOrAppendStatusFooter`；通过
- [x] **TTL 可配 / 缺 [audit] 默认 24h / 非法 duration 失败** — `runtimeconfig` 默认值测试 + `TestLoadFileAcceptsAuditSection` + `resolveAudit` 正数校验；通过
- [x] **`/audit status`** — `TestPrintAuditStatusShowsSwitchAndTTL`、`TestRunHandlesAuditCommands`；通过

非前端改动，无需浏览器验证。

## 4. 术语一致性

对照方案第 0 / 2.1 节命名 grep 代码：
- 审计 key 段 `:audit:time:` / `:audit:body:`：仅 `internal/audit/audit.go` 两处构造，一致 ✓
- `/audit` 命令：`commands.go`/`console.go` 命名一致，子命令 `enable|disable|status` ✓
- `AuditEnabled` / `audit_enabled`（JSON）与 `protection_enabled` 对称 ✓
- 防冲突：`redisotel`/`go-redis/extra` 禁用词 grep 无命中 ✓

## 5. 架构归并

实际写入 `.codestable/architecture/ARCHITECTURE.md`（非贴链接）：
- [x] §0 术语：新增「消息 ID」「审计能力（Audit）」「审计 Redis key」「审计开关状态文件」
- [x] §2 结构与交互：扩写 `internal/sender` 段（ID 行 + 审计写入）；新增 `internal/audit` 段（Recorder/Record/span/StateStore）
- [x] §3 数据与状态：新增审计 key schema + `audit.json` 段（恢复语义、与保护并列、复用 [redis]）
- [x] §4 关键决策：新增 ID 无条件拼接、审计 fail-open vs 保护 fail-closed、运行期开关+持久化+一次性恢复、复用 [redis]+key_prefix、Recorder 不用 generation；**修订** telemetry 决策为「audit Redis 在有入站 span 时追踪、仍不产生 root span、不引入 redisotel」
- [x] §5 代码锚点：新增 `SendProtectedTextWithOptions` / `audit.Recorder` / `audit.StateStore`
- [x] §6 已知约束：新增「ID/审计仅普通文本成功发送、排除 reminder/typing、双 fail-open、[audit] 可选缺省 24h」「审计恢复一次、body key 含完整正文受 body_ttl 控制」；部署脚本约束补 `[audit]`
- [x] §7 相关文档：补 `docs/user/runtime-config.md`
- [x] frontmatter：`last_reviewed` → 2026-06-13，`summary`/`tags` 增补 audit/uuid

`.codestable/attention.md`：无需新增（部署脚本/docs 同步要求已存在且本次已遵守）。

## 6. requirement 回写

`requirement: bot-message-bridge`（current）。本次新增用户可感能力（消息 ID + 审计），按 update 刷新，保留原始愿景：
- [x] 新增 2 条用户故事（消息追溯 ID、可选发送审计）
- [x] 「怎么解决」新增消息 ID + 审计段（含迁移说明、持久化、otel、fail-open、范围排除）
- [x] 「边界」新增 6 条（作用范围、独立开关、无内容查询命令、body key 敏感+TTL、audit span 门控）
- [x] 「变更记录」新增 2026-06-13 条目
- [x] frontmatter：`last_reviewed` → 2026-06-13，tags 加 audit/uuid

## 7. roadmap 回写

design frontmatter 无 `roadmap` / `roadmap_item` → 非 roadmap 起头，跳过。

## 8. attention.md 候选盘点

本 feature 未暴露需要补入 attention.md 的内容。部署脚本 + `docs/user/*` 的同步要求已在 attention.md「命令与脚本陷阱」分节记录，本次已遵守；audit 相关决策（fail-open、span 门控、不用 redisotel）属业务/设计层，归 design/req，不属"每个 feature 都会撞一次"的环境类信息。

## 9. 遗留

- 已知限制：`audit:body` key 在 Redis 明文保存完整发送正文，保留期仅由 `audit.body_ttl` 控制；共享 Redis 环境需结合 ACL / `key_prefix` 隔离与 TTL 策略评估数据驻留风险。
- 超出范围观察（design §2.5，不阻塞本 feature）：`internal/audit` 对 `protection.NewRedisClient` 的依赖、与 `protection.StateStore` 的原子写重复——若后续有第三方复用需求，可走 `cs-refactor` 抽 `internal/redisclient` / `internal/statefile`。
- 无新开 issue。
