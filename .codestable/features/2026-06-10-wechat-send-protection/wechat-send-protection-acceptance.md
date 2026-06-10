---
doc_type: feature-acceptance
feature: 2026-06-10-wechat-send-protection
status: accepted
summary: 验收默认关闭的微信发送保护模式，Redis 原子预留、冻结提醒、主动对话解冻与文档回写已完成
tags: [messaging, protection, redis, config]
accepted_at: 2026-06-10
---

# wechat-send-protection 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-10
> 关联方案 doc：`.codestable/features/2026-06-10-wechat-send-protection/wechat-send-protection-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] HTTP API 普通发送，保护开启且 `out_count=8`：第 9 条普通消息先经 `ReserveNormalSend` 原子预留，随后真实发送普通文本，再发送保护提醒并 `RecordReminderSend` 计数。代码：`internal/sender/protected_text.go:22`、`internal/protection/redis_guard.go:156`；测试：`internal/protection/redis_guard_test.go:56`、`internal/api/server_test.go:14`。
- [x] HTTP API 普通发送，`frozen=1`：`SendProtectedText` 返回 protection rejection，API 映射为 HTTP 429，且不调用 iLink 发送用户原文。代码：`internal/api/server.go:109`、`internal/api/server.go:121`；测试：`internal/api/server_test.go:38`。
- [x] 监听主动对话：`persistUpdateState` 保存 `IlinkUserID` / `ContextToken` 后调用 `RecordActiveConversation`，Redis 清零计数并重设 active TTL。代码：`internal/app/app.go:379`、`internal/app/app.go:408`、`internal/protection/redis_guard.go:77`；测试：`internal/app/app_test.go:13`、`internal/protection/redis_guard_test.go:14`。
- [x] 后台 24h 检查：`checkProtectionTimeWindowOnce` 调用 `CheckTimeWindow`，TTL 进入警戒窗口时发送一次提醒并冻结，重复检查转为 reject。代码：`internal/app/app.go:318`、`internal/protection/redis_guard.go:203`；测试：`internal/app/send_text_test.go:124`、`internal/protection/redis_guard_test.go:236`。

**名词层“现状 → 变化”逐项核对**：
- [x] Runtime config 新增 `ProtectionConfig` 与 `RedisConfig`，默认保护关闭；开启后校验 Redis URL、password 冲突、限制数值和 duration。代码：`internal/runtimeconfig/config.go:34`、`internal/runtimeconfig/config.go:66`、`internal/runtimeconfig/config.go:191`；测试：`internal/runtimeconfig/config_test.go:13`、`internal/runtimeconfig/config_test.go:139`。
- [x] 保护状态字段落在 Redis Hash / TTL String，不进入 auth store。代码：`internal/protection/redis_guard.go:94`、`internal/protection/redis_guard.go:156`、`internal/protection/redis_guard.go:224`；反向核对：`internal/config/store.go:21` 的 `UserConfig` 未新增保护字段。
- [x] Guard 契约提供 `ReserveNormalSend`、`ReleaseNormalSend`、`RecordReminderSend`、`RecordActiveConversation`、`CheckTimeWindow`。代码：`internal/protection/guard.go:39`。
- [x] Redis key 使用 `{prefix}:protect:{<botID>}:state` 和 `{prefix}:protect:{<botID>}:active`，规则全局共用、状态按 bot 隔离。代码：`internal/protection/redis_guard.go:94`；测试：`internal/protection/redis_guard_test.go:262`。

**流程图核对**：
- [x] 启动路径 `protection.enabled=false` → `NoopGuard`，`true` → `NewRedisClient` + `Ping` + `NewRedisGuard`，代码落点完整：`cmd/webot-msg/main.go:58`、`cmd/webot-msg/main.go:161`。
- [x] 普通文本发送请求 → `ReserveNormalSend` → iLink `SendMessage` → 必要时 `SendProtectionReminder` → `RecordReminderSend`，代码落点完整：`internal/sender/protected_text.go:22`。
- [x] iLink 普通文本发送失败 → `ReleaseNormalSend`，代码落点完整：`internal/sender/protected_text.go:45`；测试：`internal/app/send_text_test.go:90`、`internal/protection/redis_guard_test.go:172`。
- [x] GetUpdates 主动消息 → 保存上下文 → `RecordActiveConversation`，代码落点完整：`internal/app/app.go:379`。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 用户可在 Runtime config 开启保护模式并配置 Redis；默认关闭，不配置 Redis 时仍可解析默认配置。证据：`internal/runtimeconfig/config_test.go:13`。
- [x] 普通文本和提醒都计入下发次数：普通发送由 `ReserveNormalSend` 预留递增，提醒成功由 `RecordReminderSend` 递增。证据：`internal/protection/redis_guard_test.go:56`。
- [x] 每次主动对话后重置计数和 24h 窗口。证据：`internal/protection/redis_guard_test.go:14`。
- [x] 第 9 条普通消息触发第 10 条提醒并冻结。证据：`internal/protection/redis_guard_test.go:56`、`internal/api/server_test.go:14`、`internal/app/send_text_test.go:16`。
- [x] active TTL 进入 `time_warning_before` 时提醒并冻结。证据：`internal/protection/redis_guard_test.go:211`、`internal/protection/redis_guard_test.go:236`。
- [x] 冻结期间 API 返回 429，控制台返回明确 protection lock 错误。证据：`internal/api/server_test.go:38`、`internal/app/send_text_test.go:55`。
- [x] Redis 不可用或状态不可读写时 fail closed。证据：`internal/protection/redis_guard_test.go:295`、`internal/sender/protected_text_test.go:12`。

**明确不做逐项核对**：
- [x] 不绕过微信限制，也不模拟或伪造用户主动对话；grep 结果显示主动对话重置只来自 `GetUpdates` 保存上下文后的 `RecordActiveConversation`，无模拟微信 App 发消息实现。
- [x] 不改变 iLink `sendmessage` 请求结构；`internal/ilink/client.go:178` 未被本 feature 修改，保护逻辑位于 `internal/sender` / `internal/protection`。
- [x] 不把保护状态写入 auth store JSON；`UserConfig` 未新增 Redis / protection 字段。
- [x] 不改变 HTTP API 鉴权方式；`handleBotAction` 仍校验 `APIToken` 后才分发 action。代码：`internal/api/server.go:83`。
- [x] 不把 Redis password、BotToken、APIToken、ContextToken 或完整消息正文写入新增保护日志；新增保护日志只包含 botID、reason 和错误类别。代码：`internal/sender/protected_text.go:73`、`internal/app/app.go:326`。
- [x] typing 不计入保护限制且冻结不阻止 typing；`handleTyping` 未经过 `SendProtectedText`。代码：`internal/api/server.go:129`。
- [x] 不支持多会话独立计数；保护 key 只按 botID 生成，仍使用当前 bot 最近 `IlinkUserID` / `ContextToken`。

**关键决策落地**：
- [x] 保护逻辑在本地发送编排层：`internal/sender` 承载发送流程，`internal/ilink` 不知道保护模式。
- [x] API 和控制台共享保护服务：`internal/api/server.go:109` 与 `internal/app/app.go:200` 都调用 `sender.SendProtectedText`。
- [x] Redis SDK 使用 `github.com/redis/go-redis/v9`：`go.mod` 和 `internal/protection/redis_guard.go:8`。
- [x] 保护关闭不连接 Redis：`buildProtectionGuard` 在 disabled 时返回 `NoopGuard`。代码：`cmd/webot-msg/main.go:161`。
- [x] Redis URL 和 password 冲突失败且错误脱敏。代码：`internal/runtimeconfig/config.go:395`、`internal/protection/redis_guard.go:29`；测试：`internal/runtimeconfig/config_test.go:157`、`internal/protection/redis_guard_test.go:46`。
- [x] 多步状态转换使用 Lua 脚本原子执行。代码：`internal/protection/redis_guard.go:156`、`internal/protection/redis_guard.go:187`、`internal/protection/redis_guard.go:203`、`internal/protection/redis_guard.go:224`。

**编排层“现状 → 变化”逐项核对**：
- [x] 启动期根据 `protection.enabled` 创建 Redis guard，并把 guard / reminder text / check interval 注入 app。代码：`cmd/webot-msg/main.go:58`、`internal/app/app.go:33`。
- [x] API server 和 `App.SendText` 统一走 `internal/sender`。代码：`internal/api/server.go:109`、`internal/app/app.go:200`。
- [x] `monitorWeixin` 收到有效主动消息后重置 Redis 状态。代码：`internal/app/app.go:379`、`internal/app/app.go:408`。
- [x] 每个 bot 启动轻量时间窗口检查器。代码：`internal/app/app.go:278`、`internal/app/app.go:281`。
- [x] 保护提醒直接调用 iLink，不递归普通 guard，成功后仍计数。代码：`internal/sender/protected_text.go:66`。

**流程级约束核对**：
- [x] 错误语义：保护拒绝映射为 HTTP 429 / console 错误；非保护错误返回 500 或 console error。代码：`internal/api/server.go:109`、`internal/app/app.go:200`。
- [x] 幂等性：主动对话重置可重复执行，最终保持 `out_count=0`、`frozen=0`、active TTL 重设。测试：`internal/protection/redis_guard_test.go:14`。
- [x] 并发：普通发送预留和冻结由单个 Redis Lua 脚本完成，临界并发测试通过。测试：`internal/protection/redis_guard_test.go:120`。
- [x] 顺序：发送前预留、发送失败释放、提醒成功计数；提醒记录失败不静默成功。测试：`internal/app/send_text_test.go:90`、`internal/sender/protected_text_test.go:12`。
- [x] 可观测点：保护日志不打印 token、context token 或用户原始消息正文。
- [x] 兼容性：保护关闭路径使用 no-op guard，不创建 Redis client，不启动检查器。

**挂载点反向核对（可卸载性）**：
- [x] 挂载点 M1：TOML protection keys。实际落点：`internal/runtimeconfig/config.go:66`、`docs/user/runtime-config.md`、`scripts/linux-service.sh`。
- [x] 挂载点 M2：TOML redis keys。实际落点：`internal/runtimeconfig/config.go:79`、`docs/user/runtime-config.md`、`scripts/linux-service.sh`。
- [x] 挂载点 M3：HTTP API / 控制台普通文本发送。实际落点：`internal/api/server.go:109`、`internal/app/app.go:200`。
- [x] 挂载点 M4：主动对话事件。实际落点：`internal/app/app.go:379`、`internal/app/app.go:408`。
- [x] 挂载点 M5：时间窗口检查器。实际落点：`internal/app/app.go:278`、`internal/app/app.go:281`。
- [x] 反向 grep：`rg "ProtectionConfig|RedisConfig|ReserveNormalSend|ReleaseNormalSend|RecordReminderSend|RecordActiveConversation|CheckTimeWindow|SendProtectedText|buildProtectionGuard|redis\\.url|redis\\.password|protection\\.enabled"` 命中均落在上述挂载点、保护模块、测试、用户文档和 CodeStable 文档内，未发现漏记挂载点。
- [x] 拔除沙盘推演：删除本 feature 需要移除 Runtime config protection/redis 字段、main 的 guard 构造、App/API 的 sender 接入、`internal/protection`、`internal/sender`、时间检查器、主动对话 reset hook、测试和文档；未发现隐藏在 `internal/ilink` 或 auth store 的残留。

## 3. 验收场景核对

- [x] 默认配置或保护关闭，不配置 Redis，现有 HTTP / 控制台文本发送行为不变。
  - 证据来源：`NoopGuard` + runtime config 默认测试；`GOPROXY=off go test ./...`。
  - 结果：通过。
- [x] `enabled=true` 但 Redis URL 缺失、格式非法或认证失败，错误指向 Redis 且不泄露 password。
  - 证据来源：`TestResolveRejectsInvalidValues`、`TestResolveRedactsRedisURLParseErrors`、`TestNewRedisClientRedactsParseErrors`、`buildProtectionGuard` ping 路径。
  - 结果：通过。
- [x] `redis.url` 不含密码且 `redis.password=secret` 时，go-redis client 使用 password。
  - 证据来源：`TestNewRedisClientUsesPasswordConfig`、`TestResolveProtectionRedisConfig`。
  - 结果：通过。
- [x] `redis.url` 自带 password 且 `redis.password` 非空时启动失败，错误指向冲突。
  - 证据来源：`TestResolveRejectsInvalidValues` 的 `redis url password conflict`。
  - 结果：通过。
- [x] Redis 可用且用户刚主动对话后，`out_count=0`、active TTL 约 24h、`frozen=0`。
  - 证据来源：`TestRedisGuardRecordActiveConversationResetsState`。
  - 结果：通过。
- [x] 主动对话后连续 8 条普通文本发送成功，`out_count=8`，不发送提醒，不冻结。
  - 证据来源：`TestRedisGuardReserveNormalSendTriggersReminderAndCountsReminder` 前 8 次 reserve。
  - 结果：通过。
- [x] 主动对话后第 9 条普通文本成功，系统发送提醒，提醒计入下发次数，`frozen=1 reason=count`。
  - 证据来源：`TestRedisGuardReserveNormalSendTriggersReminderAndCountsReminder`、`TestHandleSendMessageSendsReminderAfterNormalSendDecision`、`TestSendTextSendsReminderAfterNormalSendDecision`。
  - 结果：通过。
- [x] 多 bot 独立计数。
  - 证据来源：`TestRedisGuardBotStateIsIsolated`。
  - 结果：通过。
- [x] 冻结后 HTTP API 普通文本发送返回 429，iLink 不发送用户原文。
  - 证据来源：`TestHandleSendMessageRejectsFrozenBeforeSendingUserText`。
  - 结果：通过。
- [x] 冻结后控制台普通文本返回保护错误，不发送用户原文。
  - 证据来源：`TestSendTextRejectsFrozenBeforeSendingUserText`。
  - 结果：通过。
- [x] 冻结后微信 App 主动消息到达，auth store 更新上下文，Redis 清零并解除冻结。
  - 证据来源：`TestPersistUpdateStateStoresReplyTargetAndContextTogether`、`TestRedisGuardRecordActiveConversationResetsState`。
  - 结果：通过。
- [x] active TTL 小于等于 `time_warning_before` 且未冻结，后台检查器发送一次提醒并冻结，重复检查不重复提醒。
  - 证据来源：`TestCheckProtectionTimeWindowOnceSendsReminder`、`TestRedisGuardCheckTimeWindowSendsSingleReminder`。
  - 结果：通过。
- [x] active key 缺失但保护开启，普通文本发送被拒绝并提示需要主动对话初始化。
  - 证据来源：`TestRedisGuardReserveNormalSendRejectsFrozenAndMissingActive`。
  - 结果：通过。
- [x] 普通文本 iLink 发送失败时释放预留，不增加 `out_count`，不触发提醒。
  - 证据来源：`TestSendTextReleasesReservationWhenSendFails`、`TestRedisGuardReleaseNormalSendRestoresCriticalReservation`。
  - 结果：通过。
- [x] 提醒 iLink 发送失败时保持冻结，只记录非敏感错误类别，后续普通发送仍被拒绝直到主动对话。
  - 证据来源：`SendProtectionReminder` 发送失败路径只 log 并不清除 frozen；Redis 脚本在 reminder 分支已先设 frozen；冻结拒绝由 `TestRedisGuardReserveNormalSendRejectsFrozenAndMissingActive` 覆盖。
  - 结果：通过。
- [x] `/bots/{botID}/typing` 不计入 `out_count`，冻结状态不影响 typing。
  - 证据来源：`handleTyping` 不调用 sender/guard；grep `SendTyping` 路径未命中 protection guard。
  - 结果：通过。

验证命令：
- `GOPROXY=off go test ./...`：通过。
- `GOPROXY=off go vet ./...`：通过。
- `git diff --check`：通过。
- `python3 .codestable/tools/validate-yaml.py --dir .codestable/features/2026-06-10-wechat-send-protection`：通过。
- `python3 .codestable/tools/validate-yaml.py --file .codestable/architecture/ARCHITECTURE.md`：通过。
- `python3 .codestable/tools/validate-yaml.py --file .codestable/requirements/bot-message-bridge.md`：通过。
- `python3 .codestable/tools/validate-yaml.py --file .codestable/requirements/VISION.md`：通过。

## 4. 术语一致性

- 保护模式：代码中落为 `ProtectionConfig`、`protection.Guard`、`sender.SendProtectedText`，语义与设计一致。
- 主动对话：代码只在 `persistUpdateState` 遇到 `FromUserID` + `ContextToken` 后调用 `RecordActiveConversation`，没有伪造主动对话入口。
- 下发消息：普通文本和保护提醒都通过 iLink `SendMessage`，提醒成功后也调用 `RecordReminderSend` 计数。
- 冻结状态：Redis Hash 的 `frozen` / `reason` 字段驱动 API 429 和 console protection lock 错误。
- Redis 保护状态：代码中使用 `RedisGuard` 和 `{prefix}:protect:{botID}:state/active` key，术语一致。
- 防冲突：`rg "BeforeNormalSend|RecordNormalSend"` 无命中；旧的非原子两段契约已从代码和 design 中移除。

## 5. 架构归并

对照方案第 4 节，已实际更新 `.codestable/architecture/ARCHITECTURE.md`：

- [x] 术语归并：新增“保护模式 / Redis 保护状态 / 冻结状态”的当前定义。
- [x] 结构与交互：新增 `internal/sender` 普通文本发送编排层和 `internal/protection` 保护状态计算层；补充 API / 控制台发送入口经过共享 sender。
- [x] 数据与状态：补充 auth store 不保存保护状态，Redis 保存 per-bot Hash 和 active TTL；保护关闭时 Redis 不参与。
- [x] 动词骨架：补充启动期 buildProtectionGuard、每 bot 时间窗口检查器、监听主动消息重置保护状态。
- [x] 流程级约束：补充保护开启时 Redis 不可用 fail closed、普通文本发送前原子预留、提醒也算下发、typing 排除、单最近上下文模型。
- [x] `.codestable/attention.md` 评估：已有“改启动参数/默认路径/Linux 运行方式需同步脚本和部署文档”的条目覆盖本次文档联动；本 feature 未暴露新的每次启动必读注意事项。

## 6. requirement 回写

- [x] 方案 frontmatter 指向 `requirement: bot-message-bridge`，该 requirement 已是 `current`。
- [x] 本次新增用户可感的可选发送保护能力，已更新 `.codestable/requirements/bot-message-bridge.md`：补充用户故事、为什么需要、怎么解决、边界和 2026-06-10 变更记录。
- [x] `.codestable/requirements/VISION.md` 已同步 pitch，让 requirement 索引能反映可选发送保护能力。

## 7. roadmap 回写

- [x] 方案 frontmatter 没有 `roadmap` / `roadmap_item` 字段，本 feature 非 roadmap 起头。
- [x] 不需要更新 `.codestable/roadmap/` 下的 items.yaml 或 roadmap 主文档。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 `.codestable/attention.md` 的内容。

理由：Redis 配置、保护默认关闭、部署文档同步和测试命令已分别落入 Runtime config 文档、architecture、requirement 与现有 attention 的脚本/部署同步提醒；没有新的环境、代理、命令或路径陷阱需要每次 CodeStable 启动前读取。

## 9. 遗留

- 后续优化点：暂无必须跟进的 issue。
- 已知限制：iLink `SendMessage` 返回错误时，当前实现按设计释放普通文本预留；如果未来要把网络超时视为“可能已送达”，需要重新设计 reservation id 或更保守的补偿状态机。
- 已知限制：保护模式按 bot 计数，不支持同一 bot 的多会话独立计数，沿用当前最近上下文模型。
- 实现阶段顺手发现：无需要单独开 issue 的范围外问题。
- 用户终审：待用户确认。
