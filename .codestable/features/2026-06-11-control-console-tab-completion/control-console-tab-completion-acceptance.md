---
doc_type: feature-acceptance
feature: 2026-06-11-control-console-tab-completion
status: passed
accepted_at: 2026-06-11
summary: webot-msg console 在 TTY 下支持客户端行编辑和 Tab 补全，非 TTY 与 socket 上行契约保持 line mode 兼容
tags: [console, control-console, autocomplete, terminal, raw-mode, compatibility]
---

# control-console-tab-completion 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-11
> 关联方案 doc：`.codestable/features/2026-06-11-control-console-tab-completion/control-console-tab-completion-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `AttachInteractive(socketPath string, in *os.File, out *os.File) error`：已新增于 `internal/control/interactive_client.go:26`；CLI 只在 stdin/stdout 都是 TTY 时调用（`cmd/webot-msg/main.go:31`），符合“in/out 须为已确认 TTY”的契约。
- [x] output splitter 示例 1：`"Console commands:\n  /login ...\n[bot-a] > "` → 行事件 `["Console commands:", "  /login ..."]` + prompt 尾部 `"[bot-a] > "`；`TestOutputSplitterSplitsLinesAndPromptTail` 覆盖。
- [x] output splitter 示例 2：广播 `"\n[Bot: a | Message from u]: hi\n> "` → 行事件 `["", "[Bot: a | Message from u]: hi"]` + prompt 尾部 `"> "`；`TestOutputSplitterHandlesBroadcastPromptTail` 与 fake server PTY 验收覆盖。
- [x] output splitter 示例 3：`"[bot-a"` 后续 `"] > "` → prompt 尾部从半行自愈为 `"[bot-a] > "`；`TestOutputSplitterRecoversSplitPromptTail` 覆盖。
- [x] `Attach` 原样保留：签名仍为 `func Attach(socketPath string, in io.Reader, out io.Writer) error`，实现仍是 socket 与 stdio 双向 `io.Copy`，作为非 TTY 路径。
- [x] `console.TerminalLineReader` 既有方法签名零变化：`ReadLine`、`Write`、`Close` 均未改签名；仅新增最小 `SetPrompt` 方法用于 interactive attach 刷新 prompt。

**名词层“现状 -> 变化”逐项核对**：
- [x] `control.Attach`：现状能力未改，仍是 line mode 字节透传；非 TTY 分流继续调用它。
- [x] `TerminalLineReader`：仍负责本地 TTY 行编辑、`AutoCompleteCallback` 和 Ctrl+C 识别；interactive attach 复用它，不另写编辑器。
- [x] `CompleteCommandLine`：仍是补全唯一计算入口；未新增第二套候选表。
- [x] server 输出流契约：client 端用 `outputSplitter` 解释换行行事件与 prompt tail；server 侧 `internal/control/server.go` 和 `internal/console/console.go` 无 diff。

**流程图核对**：
- [x] `webot-msg console` → TTY 判定：`cmd/webot-msg/main.go:31`。
- [x] TTY → `Dial` 后 `MakeRaw`：`AttachInteractive` 先 `net.Dial`，随后 `console.NewLocalTerminalLineReader` 执行 raw mode。
- [x] 读 goroutine → splitter → `TerminalLineReader.Write` / prompt 更新：`runInteractiveSession` 的 `handleOutputChunk` 和 `readInteractiveOutput` 覆盖。
- [x] 主循环 `ReadLine` → Enter 后整行 + LF 写入 conn：`runInteractiveSession` 写入 `result.line + "\n"`。
- [x] 非 TTY → `Attach`：CLI 默认 `attach := control.Attach`，TTY 条件不满足时不替换。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] `webot-msg console` TTY 下支持 `/pro<Tab>`：临时 service + `expect` PTY 验证，行从 `/pro` 补为 `/protection `，Enter 后 server 返回 `Usage: /protection enable|disable|status`。
- [x] 补全候选与本地前台控制台同源：TTY attach 复用 `TerminalLineReader`，后者仍通过 `CompleteCommandLine` 读取 `commandSpecs`。
- [x] 编辑期间异步广播不污染提交行：fake Unix socket server 在用户输入 `/pro` 后推送广播；client 显示 `Message from u`，server 最终收到的上行只有 `/pro\n`。
- [x] 旧 server 字节流兼容：`TestRunInteractiveSessionWritesOnlySubmittedLines` 和 fake server PTY 验证均断言上行只有用户提交行 + LF。
- [x] 非 TTY 输入行为保持 line mode：`printf '/exit\n' | webot-msg console -c <临时配置>` 输出 help + prompt 后退出，状态码 0。

**明确不做逐项核对**：
- [x] 不新增 socket 协议头、模式协商或版本握手：grep `WEBOT-MSG-CONSOLE|mode header|protocol header|magic` 无新增代码命中。
- [x] 不改 server 端读写逻辑：`git diff -- internal/control/server.go internal/console/console.go` 为空。
- [x] 不扩展补全候选：client 无动态候选源，补全仍只由 `commandSpecs` / `Subcommands` 驱动。
- [x] 不给 `nc` / `socat` 直连提供补全：补全只在 CLI TTY 分流后的 client 本地 raw mode 中发生；server 仍只读整行。
- [x] 不做 SIGWINCH：grep `SIGWINCH|syscall.SIGWINCH` 无代码命中。
- [x] 不改本地前台 `console.Run` 行为：`internal/console/console.go` 无 diff，`TerminalLineReader` 只新增方法供新 client 调用。

**关键决策落地**：
- [x] 纯客户端行编辑：TTY 分支只在 client 端切 raw mode，server 不感知。
- [x] Prompt 尾部启发式：`outputSplitter` 只维护 `tail`，按最后一个 `\n` 后的未终结文本作为 prompt。
- [x] 复用 `TerminalLineReader`：interactive client 通过 `console.NewLocalTerminalLineReader` 获取行编辑器。
- [x] 持续 `ReadLine` 循环：主循环每次 `ReadLine(currentPrompt())`，Enter 后写整行再继续下一轮；空行也会写入 conn。
- [x] Ctrl+C 语义：`ErrInterrupted` 时关闭连接并返回 nil；PTY 验证 client 退出、service 存活、终端状态正常。
- [x] 终端状态安全：`Dial` 失败路径在 raw mode 前返回；service 未运行 TTY 验收显示退出码 1 且 `stty` 正常。

**编排层“现状 -> 变化”逐项核对**：
- [x] client 从两条 `io.Copy` 扩展为 TTY 下读 goroutine + ReadLine 主循环，非 TTY 保持旧 `Attach`。
- [x] server 每连接仍跑 `console.RunWithIO`，拓扑零变化。
- [x] socket 上行方向形状仍是用户行 + LF；下行由 client 本地 splitter 解释。

**流程级约束核对**：
- [x] 错误语义：连接失败、Ctrl+C、Ctrl+D、server 主动关闭均有 PTY 验收；终端恢复到 `icanon/isig/echo`。
- [x] 兼容性：非 TTY `Attach` 回归、fake old server 上行断言、无协议头 grep 均通过。
- [x] 幂等性：Tab 补全只触发 `CompleteCommandLine`，fake server 在回车前未收到用户行。
- [x] 顺序约束：下行处理集中在单个 `readInteractiveOutput` goroutine，行事件和 prompt 更新按 chunk 到达顺序串行处理。
- [x] 可观测性：client 错误向 CLI 返回；server 无新增日志点。
- [x] 卸载性：删除 `AttachInteractive`、`outputSplitter` 和 CLI TTY 分流后，`webot-msg console` 会回退到唯一 `Attach`；无配置、持久化或协议残留。

**挂载点反向核对**：
- [x] 挂载点 1：`cmd/webot-msg/main.go` console 分支 TTY 分流，代码落点一致。
- [x] 挂载点 2：`internal/control.AttachInteractive` 公开入口，代码落点一致。
- [x] 挂载点 3：client 端 `TerminalLineReader` + `AutoCompleteCallback` 挂接，代码落点一致。
- [x] 反向 grep：`AttachInteractive|runInteractiveSession|readInteractiveOutput|outputSplitter|SetPrompt|CompleteCommandLine|NewLocalTerminalLineReader|control.Attach` 命中均落在上述挂载点、既有 console 补全、测试或 SDD 文档中，无清单外运行时挂载。
- [x] 拔除沙盘推演：移除 interactive 入口、splitter 与 CLI 分流后，server、非 TTY `Attach`、API、config、auth store、Redis 均无迁移动作；仅失去 TTY control console 补全能力。

## 3. 验收场景核对

- [x] **S1**：TTY 下 `webot-msg console` 键入 `/pro` 按 Tab → 行变 `/protection `，不执行命令；Enter 后 server 返回 protection usage/结果。
  - 证据来源：临时 service + `expect` PTY；输出出现 `/protection ` 和 `Usage: /protection enable|disable|status`。
  - 结果：通过。
- [x] **S2**：`/protection st<Tab>` → `/protection status`；`/b<Tab>` → `/bot` 不加空格。
  - 证据来源：临时 service + `expect` PTY；`/protection st` 补成 `/protection status` 并返回 `Protection disabled.`；`/b` 补成 `/bot` 后 Enter 走未知 slash command 旧语义。
  - 结果：通过。
- [x] **S3**：普通文本 `hello<Tab>` → 行不变，不触发发送。
  - 证据来源：临时 service + `expect` PTY；`hello` 在按 Tab 后保持普通文本，Enter 后才返回无 active bot 错误。
  - 结果：通过。
- [x] **S4**：编辑中收到广播消息 → 消息显示在编辑行上方，编辑内容保留，提交行不被污染。
  - 证据来源：fake Unix socket server + `expect` PTY；server 在用户输入 `/pro` 后推送 `Message from u`，最终收到的上行是 `/pro\n`。
  - 结果：通过。
- [x] **S5**：`/bots` 二级 prompt 下数字输入 Tab 不动；空行回车 → server 按取消处理。
  - 证据来源：临时 service + `expect` PTY；`/bots` 后出现 `Enter number to select...`，直接回车后回到主 prompt。
  - 结果：通过。数字输入 Tab 不动由 `CompleteCommandLine` 对非 `/` 输入保持不变的单测语义覆盖。
- [x] **S6**：`/login` 期间二维码多行输出 → 正常逐行显示，会话不乱。
  - 证据来源：fake Unix socket server + `expect` PTY；提交 `/login` 后依次显示 `QR line 1`、`QR line 2`、`Active bot changed to: bot-a`，上行只收到 `/login\n`。
  - 结果：通过。
- [x] **S7**：Ctrl+C → client 退出且终端无 raw mode 残留，service 与监听继续运行。
  - 证据来源：PTY shell；发送 0x03 后输出 `STTY_NORMAL`、`STATUS=0`、`SERVICE_ALIVE`。
  - 结果：通过。
- [x] **S8**：Ctrl+D（空编辑行）→ 上行写端关闭，server 端会话结束，client 正常退出。
  - 证据来源：PTY shell；发送 0x04 后输出 `STTY_NORMAL`、`STATUS=0`、`SERVICE_ALIVE`。
  - 结果：通过。
- [x] **S9**：service 未运行时 TTY 下执行 `webot-msg console` → 报连接错误退出，终端状态无残留。
  - 证据来源：停止临时 service 后执行 console；输出 connect error、`STTY_NORMAL`、`STATUS=1`。
  - 结果：通过。
- [x] **S10**：交互会话进行中停止 service → client 感知连接关闭，恢复终端并退出，不挂死。
  - 证据来源：PTY shell；会话中 `kill` 临时 service，client 输出 `STTY_NORMAL`、`STATUS=0`、`SERVICE_DEAD`。
  - 结果：通过。
- [x] **S11**：`printf '/exit\n' | webot-msg console` → 走线模式透传，输出与现状一致。
  - 证据来源：临时 service 管道命令；输出 help + prompt，`STATUS=0`。
  - 结果：通过。
- [x] **S12**：新 client 连旧 server（fake server 断言）→ 上行字节流仅含用户输入行与换行。
  - 证据来源：`TestRunInteractiveSessionWritesOnlySubmittedLines`；fake server PTY 异步输出场景也记录 `RECEIVED=/pro`。
  - 结果：通过。

前端改动：无。

## 4. 术语一致性

- Interactive attach：代码命中 `AttachInteractive`、`interactiveLineReader`，语义均是 TTY client 本地行编辑路径。
- Output splitter：代码命中 `outputSplitter` / `outputEvents`，只负责字节流切分为整行输出与 prompt tail。
- Prompt 尾部：代码中表现为 `tail` 字段和 `prompt` 事件；没有引入 prompt 文案白名单。
- Control console line mode：代码中仍是 `control.Attach` + server `RunWithIO`，文档已收窄为非 TTY / 第三方 socket client。
- 沿用术语：`TerminalLineReader`、`CompleteCommandLine`、`commandSpecs` 均复用既有命名，没有新增第二套概念。
- 防冲突：grep `AttachInteractive|outputSplitter|SetPrompt|CompleteCommandLine|commandSpecs`，运行时代码命中均在预期文件；历史 SDD 命中仅作上下文。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已新增 `Control console interactive attach` 术语，并将 `Control console line mode` 收窄为非 TTY / `nc` / `socat` 路径。
- [x] `.codestable/architecture/ARCHITECTURE.md`：`internal/control` 段已写入 client 端两协程拓扑、output splitter、prompt tail、Enter 后整行 + LF 上行。
- [x] `.codestable/architecture/ARCHITECTURE.md`：代码锚点已补 `AttachInteractive` 和 `outputSplitter`。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已知约束已改写为“本地 stdin/stdout 都是 TTY 的控制台支持补全；管道和第三方 socket client 仍 line mode”。
- [x] `docs/user/linux-systemd-deploy.md` 与 `docs/user/runtime-config.md`：已说明 TTY 下 `webot-msg console` 支持 Tab 补全，管道 / 非 TTY 仍 line mode。
- [x] `.codestable/attention.md`：无需新增；本次未改启动参数、默认路径、构建方式或 Linux 运行方式，且已同步部署文档。

## 6. requirement 回写

- [x] design frontmatter 指向 `requirement: bot-message-bridge`，该 requirement 已是 `current`。
- [x] 本次新增用户可感能力，已更新 `.codestable/requirements/bot-message-bridge.md`：解决方案说明 `webot-msg console` 在本地 TTY 下支持 Tab 补全，边界说明非 TTY 管道和第三方 socket client 不提供按键级补全，变更记录追加本 feature。
- [x] `requirements/VISION.md` 无需更新：能力 pitch 仍覆盖“控制台或受保护 API 回复最近会话，并通过 Linux systemd 脚本管理本地运行”，无需改变索引。

## 7. roadmap 回写

- [x] 非 roadmap 起头：design frontmatter 没有 `roadmap` / `roadmap_item` 字段，跳过 roadmap items.yaml 与主文档回写。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 `attention.md` 的新内容。临时 PTY / fake server 验收是本 feature 专属证据，不属于每个 feature 都会踩的项目硬约束。

## 9. 遗留

- 后续优化点：可考虑给真实 PTY 交互补自动化回归，但当前已有验收证据，不阻塞本 feature。
- 已知限制：非 TTY 管道、脚本输入和 `nc` / `socat` 直连 control socket 仍不提供按键级 Tab 补全；这是明确 non-goal。
- 实现阶段顺手发现：`internal/app/app.go` 直接前台 TTY 路径的异步消息仍用 `fmt.Print` 直写 stdout，属既有问题；如需统一终端重绘，后续另起 issue。
