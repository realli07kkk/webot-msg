---
doc_type: feature-acceptance
feature: 2026-06-12-simplify-cli-entry
requirement: bot-message-bridge
status: accepted
accepted_at: 2026-06-12
summary: 唯一 CLI 入口已收敛为无参 webot-msg，旧子命令和 -c/-port 参数被拒绝，socket server 保留并通过 nc/socat 直连
tags: [cli, entry, console, simplify, removal]
---

# simplify-cli-entry 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-12
> 关联方案 doc：`.codestable/features/2026-06-12-simplify-cli-entry/simplify-cli-entry-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查。

**接口示例逐项核对**：
- [x] `webot-msg`（TTY）：用 `expect` 在临时 HOME 下运行 `/tmp/webot-msg-acceptance`，输出 `Loaded 1 bots.`、`Control console listening on unix://...`、前台 prompt，并可执行 `/bots`；Ctrl+C 后输出 `Received interrupt. Saving config and exiting`。
- [x] `webot-msg`（非 TTY）：后台以 stdin 关闭方式运行，service 输出 `Console closed. Service continues running.`，API 监听 TOML 端口 `36323`，control socket 可被 `nc -U` 连接并执行 `/bots`。
- [x] `webot-msg serve` / `webot-msg console`：手工二进制验证均退出码 2，stderr 为 `webot-msg does not accept arguments; run \`webot-msg\` without arguments`。
- [x] `webot-msg -port 8080` / `webot-msg -c x.toml` / `webot-msg foo bar`：手工二进制验证均退出码 2，stderr 同上。

**名词层“现状 -> 变化”逐项核对**：
- [x] `cliOptions`、`parseCLI`、`isCommand` 已删除；`rg "cliOptions|parseCLI|isCommand" cmd/webot-msg` 无命中。
- [x] `buildRuntimeConfig()` 与 `loadRuntimeConfig()` 已收敛为无参签名，固定读取 `runtimeConfigPath`，默认配置缺失时回退 `runtimeconfig.Default()`。代码：`cmd/webot-msg/main.go:91`、`cmd/webot-msg/main.go:99`。
- [x] 端口不再有 CLI 覆盖入口；`TestBuildRuntimeConfigUsesConfigPort` 验证 TOML `api.port` 生效，`TestBuildRuntimeConfigFallsBackWhenDefaultConfigMissing` 验证缺失回退默认端口。

**流程图核对**：
- [x] `main -> 有任何参数? -> stderr + exit 2` 已落地：`cmd/webot-msg/main.go:15`；`TestMainRejectsArguments` 覆盖 `serve`、`console`、`-port`、`-c`、任意串。
- [x] `main -> buildRuntimeConfig: 固定默认路径 -> PrepareStorage/log/protection -> app.New -> app.Run(cfg.API.Port)` 已落地：`cmd/webot-msg/main.go:21`、`cmd/webot-msg/main.go:26`、`cmd/webot-msg/main.go:31`、`cmd/webot-msg/main.go:46`、`cmd/webot-msg/main.go:66`。
- [x] console attach 分支已从 `main.go` 摘除；`rg "control\\.Attach|AttachInteractive\\(" cmd/webot-msg/main.go` 无命中。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 无参 `webot-msg` 是自带 service 的前台 console：TTY 验收看到 service 启动、control socket listening、console prompt、`/bots` 可执行。
- [x] 删除 serve/console 子命令和 flag：参数拒绝手工验证与 `TestMainRejectsArguments` 都通过。
- [x] 端口与配置路径只通过默认 TOML：非 TTY 验收写入临时 `~/.webot-msg/config/webot-msg.toml` 后 API 监听 `36323`；skill 文档和用户文档已改为读取默认 TOML。

**明确不做逐项核对**：
- [x] 未删除 `internal/control` client 代码：`client.go`、`interactive_client.go`、`output_splitter.go` 及测试仍存在。
- [x] 未动 `internal/control/server.go`：`app.Run` 仍调用 `control.NewServer(a.controlSocketPath, a)` 并启动 server。代码：`internal/app/app.go:133`。
- [x] 未动 auth legacy 复制迁移：`runtimeconfig.LegacyAuthPath`、`PrepareStorage` 与 legacy copy 测试仍在；本次 diff 未改 `internal/runtimeconfig/config.go`。
- [x] 未改 `legacyProtectionWarning` 判断逻辑：仍只在 `HasLegacyProtectionSettings()` 为 true 时返回提示；测试断言 `legacy [protection] config is ignored`、`/protection enable`、`once` 仍通过。
- [x] 未动 `scripts/linux-service.sh`；已按 `attention.md` 核对，service unit 本就 `ExecStart=/usr/local/bin/webot-msg` 无参。
- [x] 未改 Runtime config TOML schema：`internal/runtimeconfig` 无 diff，`Config` / `APIConfig` / `StorageConfig` / `ControlConfig` 字段未增删。
- [x] 未引入 `-version` / `-help` 新 flag；任何参数统一拒绝。

**关键决策落地**：
- [x] 决策 D1：无参 `webot-msg` = 自带 service 的前台 console，不是后台 service 客户端。代码体现：`main` 无 console attach 分支，直接构造 `app.New` 并 `Run`。
- [x] 决策 D2：保留 socket server，只去掉客户端入口。代码体现：`internal/app/app.go:133` 仍启动 server；`main.go` 不再 import `internal/control`。

**编排层“现状 -> 变化”逐项核对**：
- [x] `main()` 开头参数校验已放在任何 Runtime config 读取之前。
- [x] 删除 `term.IsTerminal`、`control.Attach`、`AttachInteractive` 分发，相关 import 已消失。
- [x] 端口从 `resolved.API.Port` 进入 `application.Run`，没有 `opts.port` 中间态。
- [x] 非 TTY 无 bot 文案已改为 `Connect to unix://... with socat or nc and run /login`。代码：`internal/app/app.go:117`。
- [x] legacy protection warning 文案已从旧客户端入口改为 `in the console`。代码：`cmd/webot-msg/main.go:86`。

**流程级约束核对**：
- [x] 错误语义：带任意参数统一 exit 2 + stderr 一行错误；无 stdout。
- [x] 兼容性：破坏性 CLI 删除为 SDD 已批准变更；旧脚本不再静默走旧行为，改为显式失败。
- [x] 可观测性：无参启动仍打印 `Control console listening on unix://...` 和 API listen 地址；非 TTY 无 bot 提示给出 socket 连接方式。
- [x] 安全性：启动摘要仍不打印 token；测试 fake auth 仅写入临时 HOME 并已清理。

**挂载点反向核对（可卸载性）**：
- [x] 挂载点 M1：`cmd/webot-msg/main.go` 参数校验分支，落点 `main.go:15`。
- [x] 挂载点 M2：`parseCLI` / `isCommand` / console 分发删除，grep 代码无残留。
- [x] 挂载点 M3：`internal/app/app.go:117` 用户提示文案，已不提旧客户端入口。
- [x] 挂载点 M4：活文档 `runtime-config.md`、`linux-systemd-deploy.md`、`AGENTS.md`、`skill/webot-msg-send/SKILL.md` 已同步默认 TOML / socket 直连。
- [x] 反向 grep：`rg --case-sensitive "webot-msg console|webot-msg[^\n]*(-c|-port)|(-c|-port)[^\n]*webot-msg"` 对活文档、架构和 requirement 无命中。
- [x] 拔除沙盘推演：要回滚本 feature，只需恢复 `main.go` 的 CLI parser/attach 分支、旧测试和四份活文档；socket server、client 休眠库、runtimeconfig schema、auth 迁移无需迁移。

## 3. 验收场景核对

- [x] **C1**：TTY 下运行无参 `webot-msg` -> 启动 service、进入前台 console、可执行 `/bots`。
  - 证据来源：`expect` PTY 手工验收，临时 HOME + fake auth，输出包含 `Loaded 1 bots.`、`Control console listening on unix://...`、`Logged in bots:`，Ctrl+C 正常保存退出。
  - 结果：通过。
- [x] **C2**：非 TTY 下无参 `webot-msg` -> service 正常启动、不因扫码阻塞，control socket 正常开放。
  - 证据来源：后台 stdin 关闭启动 + `nc -U "$socket"` 执行 `/bots`，输出 help、prompt 和 bot 列表；service 输出 `Console closed. Service continues running.`。
  - 结果：通过。
- [x] **C3**：默认配置含 `api.port = N` 时监听 N；默认配置缺失时回退内置默认端口。
  - 证据来源：非 TTY 验收监听 `http://0.0.0.0:36323`；单测 `TestBuildRuntimeConfigUsesConfigPort` / `TestBuildRuntimeConfigFallsBackWhenDefaultConfigMissing`。
  - 结果：通过。
- [x] **C4**：`webot-msg serve` -> 非零退出 + stderr 一行错误。
  - 证据来源：手工二进制验证，exit 2。
  - 结果：通过。
- [x] **C5**：`webot-msg console` -> 非零退出 + stderr 错误。
  - 证据来源：手工二进制验证，exit 2。
  - 结果：通过。
- [x] **C6**：`webot-msg -port 8080`、`webot-msg -c x.toml`、`webot-msg foo bar` -> 均非零退出 + stderr 错误。
  - 证据来源：手工二进制验证，三者均 exit 2；单测覆盖同组输入。
  - 结果：通过。
- [x] **N1**：`internal/control/server.go` 仍在 `app.Run` 中被启动。
  - 证据来源：`internal/app/app.go:133` 调用 `control.NewServer`；C1/C2 手工验收均观察到 socket listening。
  - 结果：通过。
- [x] **N2**：`internal/control/{client,interactive_client,output_splitter}.go` 文件仍存在且全仓构建/测试通过。
  - 证据来源：`ls internal/control`；`go test ./...`、`go vet ./...` 通过。
  - 结果：通过。
- [x] **N3**：auth legacy copy 与 `legacyProtectionWarning` 判断逻辑保持不变。
  - 证据来源：`internal/runtimeconfig/config.go` 无 diff；`TestLegacyProtectionWarning` 通过。
  - 结果：通过。
- [x] **N4**：Runtime config TOML schema 不变。
  - 证据来源：`internal/runtimeconfig` 无 diff；全仓测试通过。
  - 结果：通过。

## 4. 术语一致性

- 前台 console：代码落点在 `cmd/webot-msg/main.go` 无参入口和 `internal/app/app.go` 的 `console.Run`，文档已同步为直接前台运行。
- console 客户端入口：旧 CLI 入口已从 `main.go` 删除；长期文档不再把它描述为当前能力。
- socket server：`internal/control/server.go` 保留，`app.Run` 仍启动；架构文档已明确第一方 attach 命令移除但 socket server 保留。
- 防冲突 grep：`cliOptions|parseCLI|isCommand|webot-msg console` 在 `cmd/`、`internal/`、`docs/user/`、`skill/`、architecture、requirement 当前用法中无残留；`AttachInteractive` 仅作为休眠库函数名保留。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已更新第 0 节术语，新增/调整“前台 console”“Control console”“Control client 休眠库”“Control console line mode”的当前形态。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已更新第 2 节结构与交互，说明 `cmd/webot-msg/main.go` 只接受无参运行、任何参数 exit 2、配置固定默认路径、API 端口只来自 TOML 或默认值。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已更新第 4 节关键决策，配置入口不再有 CLI flag / 子命令 / env，systemd 交互使用 `nc -U` / `socat` 连接 Unix socket。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已更新第 5/6 节代码锚点与边界，删除旧参数覆盖和旧 attach 命令作为当前能力的描述。
- [x] `.codestable/attention.md`：无需更新。已有注意事项已覆盖“改启动参数/默认路径/构建方式/Linux 运行方式时同步检查脚本和 systemd 文档”，本次已执行。

## 6. requirement 回写

- [x] `requirement: bot-message-bridge` 指向 current req，本次改变用户视角的 CLI 启动和 systemd 控制台进入方式，因此已更新 `.codestable/requirements/bot-message-bridge.md`。
- [x] 已更新“怎么解决”：无参前台 `webot-msg`、systemd 下通过 `nc -U` / `socat` 连接 socket、配置固定默认 TOML。
- [x] 已更新“边界”：不再提供 CLI 参数、子命令或环境变量配置入口；无第一方 attach 命令；Tab 补全只属于直接前台 TTY 控制台。
- [x] 已追加 2026-06-12 变更记录，并把历史条目改为不误导当前能力。
- [x] `.codestable/requirements/VISION.md` pitch 不需要更新；能力范围仍是本地登录、控制台/API 回复、TOML、保护和 systemd 管理。

## 7. roadmap 回写

- [x] 方案 frontmatter 没有 `roadmap` / `roadmap_item` 字段；非 roadmap 起头，跳过。
- [x] `.codestable/roadmap/` 当前只有 `.gitkeep`，无 items.yaml 需要同步。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 attention.md 的内容。
  - 说明：`attention.md` 现有条目已经提醒启动参数、默认路径、构建方式或 Linux 运行方式变更时同步检查 `scripts/linux-service.sh` 和 `docs/user/linux-systemd-deploy.md`；本次没有新增下个 feature 必踩的环境/命令约束。

## 9. 遗留

- 后续优化点：无必须项。
- 已知限制：旧 `internal/control` client helper 现在是休眠库；这是 design 明确保留项。未来若确认不会恢复 attach 能力，可另起 `cs-refactor` 清理。
- 实现阶段顺手发现：无。
- 验证命令：
  - `expect` TTY 验收：通过。
  - 非 TTY + `nc -U` socket 验收：通过。
  - 参数拒绝手工验收：通过。
  - `go test ./...`：通过。
  - `go vet ./...`：通过。
  - `git diff --check`：通过。
  - `python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-06-12-simplify-cli-entry/simplify-cli-entry-checklist.yaml`：通过。
