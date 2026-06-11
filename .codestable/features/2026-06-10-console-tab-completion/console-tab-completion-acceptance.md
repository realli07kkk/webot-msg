---
doc_type: feature-acceptance
feature: 2026-06-10-console-tab-completion
status: passed
accepted_at: 2026-06-11
summary: 直接前台 TTY console 支持静态命令 Tab 补全，control console 保持兼容 line mode
tags: [console, autocomplete, terminal, compatibility]
---

# console-tab-completion 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-11
> 关联方案 doc：`.codestable/features/2026-06-10-console-tab-completion/console-tab-completion-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `CommandSpec`（`internal/console/commands.go:3`）：表达 `Name`、`Usage`、`Subcommands`，当前声明 `/login`、`/bots`、`/bot`、`/del`、`/protection`、`/exit`、`/quit` 和 `/protection enable|disable|status`，与方案一致。
- [x] `CompleteCommandLine(line, pos)`（`internal/console/completion.go:8`）：返回替换行、cursor 位置和处理标记；单测覆盖 `/log`、`/ex`、`/pro`、`/b`、`/protection st`、未知命令、普通文本和 suffix 保留（`internal/console/completion_test.go:10`）。
- [x] `LineReader`（`internal/console/line_reader.go:13`）：提供 `ReadLine(prompt)` 和 `Close()`，console loop 不再绑定具体输入实现。
- [x] `BufferedLineReader`（`internal/console/line_reader.go:18`）：保持按行读取，`RunWithIO` 仍使用它，非 TTY、测试和 control session 保持 line mode。
- [x] `TerminalLineReader`（`internal/console/terminal_reader.go:12`）：基于 `golang.org/x/term.Terminal`，绑定 `AutoCompleteCallback` 并在本地 TTY raw-mode 下启用。
- [x] Control console line mode：`control.Attach` 只做 socket 双向复制（`internal/control/client.go:9`），server 仍调用 `console.RunWithIO`（`internal/control/server.go:57`），无 mode header。

**名词层“设计时现状 -> 变化”逐项核对**：
- [x] help 文本和补全候选由同一份 `commandSpecs` 提供，`RunWithLineReader` 打印 help 时遍历该表（`internal/console/console.go:48`）。
- [x] 命令执行仍在 `RunWithLineReader` 的完整行分发中完成，补全函数不接触 `Controller`。
- [x] Control console 没有新增协议探测、header 或 raw-mode 分支。

**流程图核对**：
- [x] TTY 判定、raw mode、`AutoCompleteCallback`、`ReadLine`、Enter 后命令分发均有实际落点：`internal/console/console.go:32`、`internal/console/terminal_reader.go:36`、`internal/console/terminal_reader.go:22`、`internal/console/console.go:62`。
- [x] `webot-msg console` 旧字节流转发和 server line mode 有实际落点：`internal/control/client.go:9`、`internal/control/server.go:57`。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 直接前台 TTY console 支持 `/log<Tab>`、`/pro<Tab>`、`/protection st<Tab>`：`TestTerminalLineReaderCompletesTabBeforeEnter` 覆盖（`internal/console/completion_test.go:86`）。
- [x] `webot-msg console` 保持 line mode，不向旧 server 写协议头：`TestAttachDoesNotWriteControlHeader` 覆盖（`internal/control/client_test.go:11`）。
- [x] 非 TTY 与测试继续按行读取：`RunWithIO` 仍绑定 `NewBufferedLineReader`（`internal/console/console.go:39`），既有 console 测试继续通过。
- [x] 普通文本 Tab 不触发命令补全：`TestCompleteCommandLineLeavesTextUnchanged` 覆盖（`internal/console/completion_test.go:66`）。

**明确不做逐项核对**：
- [x] 不补全 bot 序号、botID、Redis 配置、文件路径、历史消息或普通文本：`CompleteCommandLine` 只扫描 `commandSpecs` 和固定 `Subcommands`。
- [x] 不新增 Bash/Zsh shell completion：未新增 shell completion 文件或 CLI completion 命令。
- [x] 不改成完整命令框架：命令执行仍是 `RunWithLineReader` 内的轻量分发。
- [x] 不新增 HTTP API、配置项、auth store 字段或持久化状态：本次 diff 未触及 API 路由、config schema 或 store schema。
- [x] 不在 `webot-msg console`、`nc`、`socat` control socket 路径提供按键级 Tab 补全：control server 仍是 `RunWithIO` line mode。

**关键决策落地**：
- [x] 复用 `golang.org/x/term.Terminal`，没有新增 readline 依赖。
- [x] help 和 completion 共用 `commandSpecs`。
- [x] `RunWithIO` 作为 line mode 兼容入口保留。
- [x] Control console 不做 Tab 补全，避免 client-first header 兼容事故。
- [x] Ctrl+C 建模为 `ErrInterrupted` / `ExitReasonInterrupt`，前台 service 收到后保存配置并退出（`internal/app/app.go:138`）。

**编排层变化核对**：
- [x] console loop 只替换“读一行”的抽象，命令执行路径保持 Enter 后完整行分发。
- [x] TTY raw-mode 路径由 `NewLocalTerminalLineReader` 负责，raw mode 失败时返回 false 并 fallback line mode。
- [x] Control console 路径没有 mode header、deadline 探测或 session mode 状态。

**流程级约束核对**：
- [x] 错误语义：Ctrl+C 返回 `ErrInterrupted`，普通 EOF 返回 `ExitReasonInputClosed`；二者由测试覆盖（`internal/console/console_test.go:35`、`internal/console/completion_test.go:113`）。
- [x] 幂等性：补全函数只做字符串计算，不调用 `Controller`。
- [x] 安全性：补全候选只来自静态命令表，不读取凭据、Redis 配置或消息内容。
- [x] 可观测性：新增命令漏接补全可通过 `commandSpecs` 与 completion 测试暴露。
- [x] 卸载性：删除 `terminal_reader.go`、`completion.go` 和 `commands.go` 后，`RunWithIO` 可回到 buffered 行读取；control socket 无协议残留。

**挂载点反向核对**：
- [x] Console 交互输入层：落点为 `Run` / `RunWithLineReader` / `LineReader`。
- [x] Console 命令候选源：落点为 `commandSpecs`。
- [x] Terminal completer：落点为 `TerminalLineReader.AutoCompleteCallback`。
- [x] Control console 兼容守护：落点为 `Attach` 测试和 server `RunWithIO`。
- [x] grep 核对：`CompleteCommandLine`、`TerminalLineReader`、`ErrInterrupted`、`mode header` 等命中均落在上述挂载点或 SDD/测试文档内，无清单外代码挂载点。
- [x] 拔除沙盘推演：移除本地 terminal/completion 文件时，control console、API、config、auth store 不需要迁移；删除 control 兼容测试不会影响协议，因为协议本身未改变。

## 3. 验收场景核对

- [x] **S1**：`/pro<Tab>` -> `/protection `，不执行命令。
  - 证据：`TestCompleteCommandLineCompletesTopLevelCommands` 和 `TestTerminalLineReaderCompletesTabBeforeEnter`。
- [x] **S2**：`/log<Tab>` -> `/login`，不执行命令。
  - 证据：同上。
- [x] **S3**：`/ex<Tab>` -> `/exit`，只有 Enter 后退出。
  - 证据：completion 单测只返回字符串；`RunWithLineReader` 只有完整行等于 `/exit` / `/quit` 时返回 `ExitReasonCommand`。
- [x] **S4**：`/protection st<Tab>` -> `/protection status`，Enter 后才查询状态。
  - 证据：`TestCompleteCommandLineCompletesProtectionSubcommands`；查询只在 `handleProtectionCommand` 的 Enter 分发后执行。
- [x] **S5**：`/b<Tab>` 最多补到 `/bot` 公共前缀。
  - 证据：`TestCompleteCommandLineCompletesTopLevelCommands` 的 shared prefix case。
- [x] **S6**：`/unknown<Tab>` 输入不变，Enter 后仍走未知 slash command 旧语义。
  - 证据：`TestCompleteCommandLineLeavesUnknownCommandUnchanged`；`RunWithLineReader` 未识别 slash command 后仍提示并走 `SendText`。
- [x] **S7**：`hello<Tab>` 不补全、不调用 `SendText`；Enter 后才按普通文本发送。
  - 证据：`TestCompleteCommandLineLeavesTextUnchanged`；补全函数无 Controller 依赖。
- [x] **S8**：`/protection ` 空子命令前缀不自动选择 `enable` / `disable` / `status`。
  - 证据：`TestCompleteCommandLineDoesNotChooseEmptySubcommandPrefix`。
- [x] **S9**：非 TTY `bytes.Buffer` 输入 `/protection status\n/exit\n` 仍按行执行。
  - 证据：既有 console 测试通过；`RunWithIO` 仍是 buffered line reader。
- [x] **S10**：新 `webot-msg console` 连旧 service 不发送 `WEBOT-MSG-CONSOLE` 协议文本。
  - 证据：`TestAttachDoesNotWriteControlHeader`。
- [x] **S11**：`printf '/exit\n' | webot-msg console` 仍是普通 line mode 输入。
  - 证据：`Attach` 不区分 TTY，control server 用 `RunWithIO`，无 raw mode/header。
- [x] **S12**：直接前台 TTY Ctrl+C 恢复 terminal 并保存退出。
  - 证据：`ErrInterrupted`、`ExitReasonInterrupt`、`App.Run` 保存退出路径；`TestTerminalLineReaderReturnsInterruptedForCtrlC` 和 `TestRestoreActiveTerminalClosesRegisteredReader` 覆盖 reader/restore 语义。
- [x] **S13**：补全期间后台广播不会执行未完成输入，不能破坏最终 Enter 后提交内容。
  - 证据：命令执行只在 `ReadLine` 返回后发生；TTY 输出走 `TerminalLineReader.Write`，交给 `x/term.Terminal.Write` 处理编辑中输出。
- [x] **反向核对**：未新增配置项、auth store 字段、HTTP API 路由、Redis key 或 iLink 请求结构。
  - 证据：diff 只涉及 console/app/control 测试和文档；`go test ./...` 通过。

前端改动：无。

## 4. 术语一致性

- Console 命令：代码中由 `CommandSpec` / `commandSpecs` 表达，术语一致。
- 子命令：代码中为 `Subcommands []string`，当前只用于 `/protection`，术语一致。
- Tab 补全：代码中为 `CompleteCommandLine` + `AutoCompleteCallback`，术语一致。
- Terminal line reader：代码中为 `TerminalLineReader`，术语一致。
- Line mode fallback：代码中为 `BufferedLineReader` / `RunWithIO`，术语一致。
- Control console line mode：代码中为 `control.Attach` + `console.RunWithIO`，术语一致。
- 防冲突：未新增 Bash/Zsh shell completion、HTTP API、config schema 或持久化命名。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md` 名词归并：已补 `Console 命令补全` 与 `Control console line mode`，说明补全范围、无副作用和 control 兼容边界。
- [x] `.codestable/architecture/ARCHITECTURE.md` 动词骨架归并：`internal/console` 段已说明 TTY 下 `TerminalLineReader` + `AutoCompleteCallback`，非 TTY 使用 `BufferedLineReader`。
- [x] `.codestable/architecture/ARCHITECTURE.md` 流程级约束归并：已知约束中说明 `/exit` 与 Ctrl+C 语义、`webot-msg console` line mode 和无按键级补全。
- [x] 用户文档归并：`docs/user/runtime-config.md` 和 `docs/user/linux-systemd-deploy.md` 已说明 direct TTY Tab 补全和 `webot-msg console` 的兼容 line mode。

归并完成后，只读 architecture 已能知道系统存在本地 TTY 补全、补全范围、control console 不补全以及 Ctrl+C 语义。

## 6. requirement 回写

- [x] 方案 frontmatter 指向 `requirement: bot-message-bridge`，该 requirement 已是 `current`。
- [x] 本次改了用户可感能力，已更新 `.codestable/requirements/bot-message-bridge.md`：用户故事、解决方案、边界和变更记录均反映 direct TTY Tab 补全与 `webot-msg console` line mode 限制。
- [x] `requirements/VISION.md` 无需更新：能力 pitch 未改变，仍归属于同一 current requirement。

## 7. roadmap 回写

- [x] 非 roadmap 起头：design frontmatter 没有 `roadmap` / `roadmap_item` 字段，跳过 roadmap items.yaml 与主文档回写。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 `attention.md` 的新内容。现有注意事项“改 Linux 运行方式要同步部署文档”已被遵守。

## 9. 遗留

- 后续优化点：control socket 如需支持 Tab 补全，必须另起设计并加入兼容协商；不能引入 client-first 裸 header。
- 已知限制：`webot-msg console`、`nc`、`socat` control socket 路径不提供按键级 Tab 补全，这是本次明确边界。
- 实现阶段顺手发现：`internal/console` 后续如果继续增加多级命令，可另走 `cs-refactor` 或独立 feature 收敛命令分发 registry；本次不做。
