---
doc_type: feature-acceptance
feature: 2026-06-11-protection-state-persistence
requirement: bot-message-bridge
status: passed
summary: /protection enable 状态已持久化到 ~/.webot-msg/state/protection.json，并在服务启动时按状态尝试一次自动恢复
tags: [protection, persistence, state, console]
---

# protection-state-persistence 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-11
> 关联方案 doc：`.codestable/features/2026-06-11-protection-state-persistence/protection-state-persistence-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**：

- [x] `protection.StateStore`（`internal/protection/state_store.go`）：`NewStateStore(path)`、`Load()`、`Save(PersistedState)` 已落地；文件不存在返回零值无错误，非法 JSON 返回零值 + error，`Save` 使用 `MkdirAll(0700)`、临时文件和 `rename`。
- [x] `protection.PersistedState`（`internal/protection/state_store.go`）：JSON 契约仅含 `protection_enabled` 布尔字段。
- [x] `runtimeconfig.DefaultProtectionStatePath`（`internal/runtimeconfig/config.go`）：默认值为 `~/.webot-msg/state/protection.json`，`Config.Resolve()` 展开 `~` 后注入 `App`；字段标记为 `toml:"-"`，不进入 TOML 契约。
- [x] `app.Options.ProtectionStatePath`（`internal/app/app.go`）：由 `cmd/webot-msg/main.go` 传入解析后的状态文件路径，`App.New` 仅在路径非空时构造 `StateStore`。

**名词层“现状 → 变化”逐项核对**：

- [x] `RuntimeGuard` 保持运行期开关职责；持久化没有塞入 Redis guard 或 Runtime config。
- [x] `App.EnableProtection` / `DisableProtection` 成功路径追加状态文件写入；写入失败只输出 warning 和日志，不改变命令成功结果。
- [x] `runtimeconfig` 新增内部 state 路径默认值和解析，不新增用户可配置 key。

**流程图核对**：

- [x] 启动路径：`App.Run` 在 `store.Load()` 后、扫码/monitor/API 前调用 `restoreProtectionState()`。
- [x] enable/disable 路径：`EnableProtection` 写 `true`，`DisableProtection` 写 `false`。
- [x] 损坏 / 缺失 / Redis 失败分支：由 `StateStore.Load` 和 `restoreProtectionState` 测试覆盖。

实现中发现的偏差：`cmd/webot-msg` 的 legacy `[protection]` warning 仍是旧措辞，验收中已改为“一次性 `/protection enable`，后续恢复”。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] `/protection enable` 成功后写入 `~/.webot-msg/state/protection.json`：`TestProtectionCommandsPersistState` 验证写入 `true`。
- [x] `/protection disable` 同步落盘关闭：同一测试验证写入 `false`。
- [x] 服务启动自动恢复：`TestRestoreProtectionStateEnablesRuntimeGuard` 验证状态为开启且 Redis 可用时 RuntimeGuard enabled，`RuntimeStatus` enabled。
- [x] 状态文件不存在保持现状：`TestRestoreProtectionStateDisabledOrMissingKeepsGuardDisabled` 验证无 warning 且 guard disabled。
- [x] Redis 不可用只尝试一次且不改写文件：`TestRestoreProtectionStateFailureDoesNotRewriteState` 验证 warning、guard disabled、文件仍为 `true`。
- [x] 非法 JSON 告警并视为关闭：`TestRestoreProtectionStateWarnsForDamagedJSON` 验证损坏告警和 guard disabled。

**明确不做逐项核对**：

- [x] 不做后台重试 / 定时重连：grep 仅命中既有保护窗口 `ticker`，无恢复 retry/reconnect 逻辑。
- [x] 不新增 TOML 状态路径 key：`ProtectionStatePath` 为 `toml:"-"`，未知 key 测试仍覆盖严格 TOML。
- [x] 状态文件不保存 Redis 配置、password、规则或 token：`PersistedState` 只有 `ProtectionEnabled bool`。
- [x] 不写入 TOML、auth store 或 Redis：状态写入仅通过 `StateStore.Save` 到本地 JSON。
- [x] 不新增 HTTP 管理接口或控制台子命令：`internal/console/commands.go` 仍只有 `enable|disable|status`。

**关键决策落地**：

- [x] 状态文件由 service 进程写入：写入发生在 `App.EnableProtection` / `DisableProtection`，control console 仍只是命令客户端。
- [x] Save 失败不阻断命令：`TestProtectionCommandsWarnWhenPersistFails` 验证 enable/disable 仍改变运行期 guard 并输出 warning。
- [x] 启动恢复在 auth store 加载后、monitor/API 前：`App.Run` 中 `restoreProtectionState` 位于 `store.Load()` 后、`startMonitor` 和 `apiServer.Start` 前。
- [x] disable 写显式 false：`TestProtectionCommandsPersistState` 验证 disable 后读取 `ProtectionEnabled=false`。
- [x] 原子替换和权限：`StateStore.Save` 使用临时文件 + rename，目录 0700、文件 0600；`TestStateStoreSaveSetsPermissions` 覆盖。

**编排层“现状 → 变化”逐项核对**：

- [x] `app.Run` 增加启动恢复步骤，缺失/false/损坏/失败/成功分支都有代码落点。
- [x] 已登录 bot 的保护检查器不在恢复函数内单独启动，而是随 `startMonitor` 走 `protectionIsEnabled()` 自然启动。
- [x] enable/disable 成功路径追加 `persistProtectionState`，失败只 warning。

**流程级约束核对**：

- [x] 错误语义：恢复失败不阻止服务启动，Save 失败不改变命令结果。
- [x] 幂等性：重复 enable/disable 重复写同一布尔状态；原子 rename 防半截文件。
- [x] 并发约束：未引入新锁层级；状态文件 last-writer-wins，和运行期 enable/disable 语义一致。
- [x] 可观测点：恢复成功 / 失败 / 损坏均写日志；交互 stderr 下失败输出 warning。Redis URL/password 不被格式化到日志。
- [x] 卸载性：删除 `StateStore`、`restoreProtectionState`、`persistProtectionState` 和默认路径注入后，保护恢复到“运行期控制，不自动恢复”的旧形态；磁盘 JSON 变成无主文件。

**挂载点反向核对**：

- [x] 状态文件挂载点：`~/.webot-msg/state/protection.json`，实际由 `DefaultProtectionStatePath` 和 `StateStore` 创建。
- [x] 启动恢复挂载点：`App.Run` 中 `restoreProtectionState`。
- [x] 持久化挂接：`EnableProtection` / `DisableProtection`。
- [x] 默认路径常量：`runtimeconfig.DefaultProtectionStatePath` 和 `Config.Resolve`。
- [x] 反向 grep：`StateStore|PersistedState|ProtectionStatePath|restoreProtectionState|persistProtectionState|protection_enabled` 命中均落在以上挂载点、测试、文档内。
- [x] 拔除沙盘推演：按挂载点逆向删除后，无新增控制台命令、HTTP 路由或 Redis schema 残留。

## 3. 验收场景核对

- [x] **S1 Redis 可用，执行 `/protection enable`**
  - 证据来源：`TestProtectionCommandsPersistState` + `TestStateStoreSaveSetsPermissions`
  - 结果：通过。状态文件内容为 `true`，目录 0700，文件 0600。
- [x] **S2 enable 后重启，Redis 可用**
  - 证据来源：`TestRestoreProtectionStateEnablesRuntimeGuard`
  - 结果：通过。恢复后 RuntimeGuard enabled，`RuntimeStatus` enabled；`startMonitor` 后会按 enabled 状态启动窗口检查器。
- [x] **S3 disable 后重启**
  - 证据来源：`TestProtectionCommandsPersistState` + `TestRestoreProtectionStateDisabledOrMissingKeepsGuardDisabled`
  - 结果：通过。文件为 `false`，恢复路径无 warning，保护关闭。
- [x] **S4 状态文件不存在**
  - 证据来源：`TestStateStoreLoadMissingFileReturnsDisabled` + `TestRestoreProtectionStateDisabledOrMissingKeepsGuardDisabled`
  - 结果：通过。视为关闭，无告警。
- [x] **S5 状态为开启但启动时 Redis 不可用**
  - 证据来源：`TestRestoreProtectionStateFailureDoesNotRewriteState`
  - 结果：通过。服务恢复函数返回，保护关闭，有 warning，文件仍为 `true`；无后台重试逻辑。
- [x] **S6 状态文件非法 JSON**
  - 证据来源：`TestStateStoreLoadDamagedJSONReturnsDisabledAndError` + `TestRestoreProtectionStateWarnsForDamagedJSON` + `TestStateStoreSaveOverwritesDamagedJSON`
  - 结果：通过。损坏时告警并关闭；后续 Save 可重写为合法内容。
- [x] **S7 状态文件路径不可写时 enable**
  - 证据来源：`TestProtectionCommandsWarnWhenPersistFails`
  - 结果：通过。命令返回 nil，运行期保护已开启，输出持久化失败 warning。
- [x] **S8 先 enable 再 disable 再重启**
  - 证据来源：`TestProtectionCommandsPersistState` + disabled 恢复测试
  - 结果：通过。最后写入者为 `false`，重启保持关闭。
- [x] **S9 恢复失败后手动 `/protection enable` 成功**
  - 证据来源：`TestRestoreProtectionStateFailureDoesNotRewriteState` 保留用户意图，`TestProtectionCommandsPersistState` 验证手动 enable 会重写 `true`
  - 结果：通过。

反向核对项：

- [x] 无后台重试 / 定时重连。
- [x] TOML 契约未新增状态路径 key。
- [x] 状态文件内容不含 Redis 配置、password、`BotToken`、`APIToken`、`ContextToken`。
- [x] 不写 TOML、auth.json 或 Redis。
- [x] 控制台命令面未新增子命令。

## 4. 术语一致性

- 保护开关状态文件：代码中对应 `DefaultProtectionStatePath`、`ProtectionStatePath`、`StateStore`，文档统一使用 `~/.webot-msg/state/protection.json`。
- 启动恢复：代码中对应 `restoreProtectionState`，无其他恢复命名分叉。
- state 目录：文档路径树新增 `state/protection.json`，runtimeconfig 默认路径一致。
- 防冲突：`State` 裸类型未新增；新持久化值对象命名为 `PersistedState`，避免与 `protection.Status` 混淆。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已新增“保护开关状态文件”术语，更新 Runtime config、保护模式、`internal/app`、`internal/protection`、数据与状态、关键决策、代码锚点和已知约束。
- [x] 名词归并：`StateStore`、`PersistedState`、`DefaultProtectionStatePath`、保护开关状态文件已写入架构。
- [x] 动词骨架归并：启动读取状态并尝试一次恢复、enable/disable 写状态已写入架构。
- [x] 流程级约束归并：恢复失败只告警并保持关闭、不后台重试、不改写状态文件、状态路径不暴露 TOML key 已写入架构。
- [x] `.codestable/attention.md`：未暴露新的每次启动都必须知道的工具/环境硬约束，无需本阶段改动。

## 6. requirement 回写

- [x] design frontmatter 指向 `requirement: bot-message-bridge`，该 req 已是 `current`。
- [x] 本次改了用户可见边界，已更新 `.codestable/requirements/bot-message-bridge.md`：发送保护状态从“不持久化/重启后需重新 enable”改为“写入 `~/.webot-msg/state/protection.json`，重启或升级后尝试一次自动恢复”。
- [x] 用户文档同步：`docs/user/runtime-config.md`、`docs/user/linux-systemd-deploy.md` 和 `scripts/linux-service.sh` 均去掉“升级后重新 enable”的旧表达。

## 7. roadmap 回写

- [x] design frontmatter 无 `roadmap` / `roadmap_item` 字段。
- [x] `.codestable/roadmap/` 仅有 `.gitkeep`，本 feature 非 roadmap 起头，跳过 roadmap items.yaml 和主文档回写。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 `attention.md` 的新内容。既有注意事项“改启动参数、默认路径、构建方式或 Linux 运行方式时同步检查脚本和用户文档”已按本次执行。

## 9. 遗留

- 后续优化点：`internal/app/app.go` 继续承担启动编排、保护生命周期和控制台广播等多职责，沿用 design 2.5 的观察，建议后续专门走 `cs-refactor`，本 feature 不处理。
- 已知限制：启动恢复只尝试一次；Redis 不可用时不会后台自动开启，用户需修复后手动 `/protection enable`。
- 实现阶段顺手发现：无新增。

## 最终验证

- `go test ./...`：通过
- `go vet ./...`：通过
- `git diff --check`：通过
