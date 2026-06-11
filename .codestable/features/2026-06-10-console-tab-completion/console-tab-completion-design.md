---
doc_type: feature-design
feature: 2026-06-10-console-tab-completion
requirement: bot-message-bridge
status: approved
summary: 在交互式 console 中支持 Tab 补全所有已声明命令和固定子命令
tags: [console, control-console, autocomplete, terminal]
---

# console-tab-completion design

## 0. 术语约定

- Console 命令：当前交互控制台里的 slash command，包括 `/login`、`/bots`、`/bot`、`/del`、`/protection`、`/exit`、`/quit`。代码锚点：`internal/console/commands.go:9`、`internal/console/console.go:78`。
- 子命令：某个 Console 命令的固定第二级 token。目前只有 `/protection enable|disable|status`。代码锚点：`internal/console/commands.go:29`、`internal/console/console.go:150`。
- Tab 补全：用户在交互式终端里按下 Tab 时，控制台根据当前光标前输入补齐所有已声明 Console 命令或固定子命令；补全本身不执行命令、不改变 active bot、不发送文本。
- Terminal line reader：面向 TTY 的按键级读取器。当前项目已经依赖 `golang.org/x/term`，其 `Terminal` 支持 `AutoCompleteCallback`，可以复用，不需要新增 readline 依赖。
- Line mode fallback：非 TTY、测试、脚本输入仍按当前 `bufio.Reader.ReadString('\n')` 行读取，不支持按键级 Tab 补全，也不改变现有输入兼容性。
- Control console line mode：`webot-msg console` 连接 Unix socket 时保持旧字节流转发语义，不发送协议头、不切 raw mode、不提供 Tab 补全；这样新 client 连接旧 service 时不会把控制字节误当作用户输入。

## 1. 决策与约束

需求摘要：当前 console 只能整行读取输入，Tab 会作为普通字符进入缓冲区，无法在用户输入 `/log`、`/ex`、`/protection st` 这类命令时补齐命令或子命令。本 feature 在交互式 console 中增加 Tab 补全，覆盖 command spec 中声明的所有 Console 命令和所有固定子命令，减少控制台命令输入成本。

成功标准：
- 在直接运行服务进入本地 console，且 stdin/stdout 是 TTY 时，按 Tab 可以补全所有已声明 Console 命令和所有已声明固定子命令。
- `webot-msg console` 进入运行中 service 时保持 line mode；不会在用户输入前向 control socket 写入任何协议头，管道输入和新 client 连旧 service 都保持兼容。
- 非 TTY 输入、单元测试和脚本管道仍按行读取；原有 `/login`、`/bots`、`/bot <num>`、`/del <num>`、`/protection ...`、`/exit`、普通文本发送语义不变。
- Tab 补全只处理以 `/` 开头的命令输入；普通文本消息中的 Tab 不触发命令补全，也不会导致消息提前发送。
- 补全逻辑有纯函数测试覆盖，能验证顶层命令、唯一匹配、公共前缀、无匹配、固定子命令和普通文本不补全。

明确不做：
- 不补全 bot 序号、botID、Redis 配置、文件路径、历史消息或用户普通文本。
- 不新增 shell completion，例如 Bash/Zsh 的 `webot-msg <TAB>`；本 feature 只做运行中 console 内部补全。
- 不把 console 改成完整命令框架；命令执行分发可以保持当前 switch/if 风格，只抽出补全需要的静态命令元数据。
- 不新增 HTTP API、配置项、auth store 字段或持久化状态。
- 不在 `webot-msg console` / control socket 会话里提供 Tab 补全；control socket 补全需要兼容协商，另起设计。

复杂度档位：走默认小功能档位，新增偏离为 Terminal Interaction = raw-mode aware（原因：Tab 是按键级行为，不能继续只靠行读取），Compatibility = fallback-required（原因：非 TTY 脚本和测试不能被 raw mode 破坏）。不涉及高并发、外部 API、持久化迁移或安全权限扩大。

关键决策：
- 复用 `golang.org/x/term.Terminal` 的 `AutoCompleteCallback`，不引入新的 readline 依赖。理由：项目已有 `golang.org/x/term`，本 feature 只需要固定命令补全，不值得新增第三方交互库。
- 补全候选集中在 `internal/console` 的静态命令元数据中，供帮助文本和 completer 共用。理由：避免 help 里新增了命令但 Tab 候选忘记同步。
- `console.RunWithIO` 保持 line mode 语义，作为测试和非 TTY 兼容入口；交互式入口通过 terminal-aware reader 包装后复用同一条命令分发流程。
- Control console 本轮保持 line mode，不做 Tab 补全。理由：control socket 已经是 userspace，本轮不能引入 client-first header 或其他旧 server 会当作用户输入的控制字节；兼容协商设计清楚前，宁可不在 control console 做补全。
- 直接前台运行 service 的本地 TTY 控制台可以进入 raw mode；Ctrl+C 必须被建模为 interrupt 并恢复 foreground 停进程语义，不能和 stdin EOF 混在一起。

已确认：
- 补全范围为所有已声明 Console 命令和所有已声明固定子命令。当前顶层命令包括 `/login`、`/bots`、`/bot`、`/del`、`/protection`、`/exit`、`/quit`；当前固定子命令包括 `/protection enable|disable|status`。后续新增命令或子命令时，进入同一份 command spec 即纳入 Tab 补全。
- 动态 bot 列表不在本次范围。
- 对歧义输入，设计采用“补到公共前缀；没有更长公共前缀时不改变输入”的保守行为。例如 `/b<Tab>` 可以补到 `/bot`，但不会自动补成 `/bot `，因为 `/bots` 也是合法命令。

补全示例：

```text
输入：/pro<Tab>
结果：/protection 

输入：/log<Tab>
结果：/login

输入：/ex<Tab>
结果：/exit

输入：/protection st<Tab>
结果：/protection status

输入：/b<Tab>
结果：/bot

输入：hello<Tab>
结果：hello
```

## 2. 名词与编排

### 2.1 名词层

设计时现状：
- `internal/console.RunWithIO` 在循环里打印 prompt，然后用 `bufio.Reader.ReadString('\n')` 读取整行；按键级 Tab 到达时机已经太晚，无法在 Enter 前补全。
- Console help 文本、命令识别、`/protection` 子命令 usage 分散在 `RunWithIO` 和 `handleProtectionCommand` 里；没有静态命令表。
- `control.Attach` 当前只做 `io.Copy`：stdin 复制到 Unix socket，socket 输出复制到 stdout。代码锚点：`internal/control/client.go:9`。
- `control.Server` 每个连接直接调用 `console.RunWithIO(s.controller, conn, out)`，control socket 会话和本地进程内 console 复用同一套命令语义。代码锚点：`internal/control/server.go:57`。

变化：
- 在 `internal/console` 新增静态命令元数据，表达顶层命令和固定子命令：

```go
type CommandSpec struct {
	Name        string
	Usage       string
	Subcommands []string
}
```

- 新增纯补全函数，输入当前行和 cursor byte offset，基于 command spec 中声明的全部顶层命令和固定子命令输出替换后的行、cursor 位置和是否处理该 Tab：

```go
func CompleteCommandLine(line string, pos int) (newLine string, newPos int, ok bool)
```

- 新增 line reader 抽象，让 console loop 不关心底层是 `bufio` 还是 `x/term`：

```go
type LineReader interface {
	ReadLine(prompt string) (string, error)
	Close() error
}
```

- `BufferedLineReader` 保持当前行读取行为，服务非 TTY、测试和脚本使用它。
- `TerminalLineReader` 基于 `golang.org/x/term.Terminal`，设置 prompt 和 `AutoCompleteCallback`；只有 TTY/raw-mode 路径使用它。
- Control console 保持现有 `io.Copy` + `RunWithIO` line mode：client 不发送 mode header，server 不做会话模式探测。

### 2.2 编排层

```mermaid
flowchart TD
  A[启动 console session] --> B{本地输入输出是否都是 TTY?}
  B -- 否 --> C[BufferedLineReader<br/>按行读取]
  B -- 是 --> D[设置 raw mode<br/>创建 x/term Terminal]
  D --> E[绑定 AutoCompleteCallback]
  C --> F[console loop 生成 prompt]
  E --> F
  F --> G[ReadLine(prompt)]
  G --> H{用户按 Tab?}
  H -- 是 --> I[CompleteCommandLine<br/>只处理已声明 Console 命令]
  I --> G
  H -- 否 --> J[用户按 Enter 返回整行]
  J --> K[复用现有命令分发和文本发送]
  K --> F

  L[webot-msg console] --> M[保持旧字节流转发]
  M --> N[server 使用 BufferedLineReader<br/>不支持按键级 Tab]
```

现状：
- 本地 console 和 control console 都把输入当作完整行处理；control client 不负责理解命令，只透传字节。
- Prompt 文本由 console loop 根据 session-local active bot 生成，active bot 不写回全局。代码锚点：`internal/console/console.go:46`、`.codestable/architecture/ARCHITECTURE.md` 第 0 节。

变化：
- console loop 仍然是唯一命令分发入口，只把“读一行”的动作替换为可插拔 LineReader。
- TTY raw-mode 下，Tab 由 `AutoCompleteCallback` 捕获；如果当前 buffer 不以 `/` 开头，返回 `ok=false` 或原样结果，让普通文本输入保持自然。
- Top-level completion 覆盖 command spec 声明的全部 `Name`；subcommand completion 覆盖 command spec 声明的全部固定 `Subcommands`。
- Control console 路径保持 line mode；`webot-msg console` 不切 raw mode，不发送 mode header，避免新 client 连旧 service 时产生业务副作用。

流程级约束：
- 错误语义：本地 TTY raw mode 设置失败时不应让 console 不可用；应回退 line mode 并禁用补全。raw mode 下 Ctrl+C 要返回明确 interrupt，不应被当作普通 EOF。
- 兼容性：非 TTY 输入不能被 `x/term` 接管；现有测试继续可以通过 `bytes.Buffer` 驱动 `RunWithIO`。
- 幂等性：Tab 补全不调用 Controller，不触发 `/login`、`SendText` 或保护开关；只有 Enter 后的完整行才进入命令分发。
- 安全性：补全候选只包含 command spec 中声明的静态 Console 命令和固定子命令，不读取或展示 `BotToken`、`APIToken`、`ContextToken`、Redis password 或消息内容。
- 可观测性：help 文本和补全候选来自同一份 command spec；新增命令时漏接补全应能被测试发现。
- 卸载性：删除 TerminalLineReader 和 CompleteCommandLine 后，console 回到当前按行读取行为；control socket 没有新增协议，配置、持久化或 API 均无残留。

### 2.3 挂载点清单

- Console 交互输入层：`internal/console` session 读行入口 — 修改为支持 terminal reader 与 line mode fallback。
- Console 命令候选源：`internal/console` 静态 command spec — 新增顶层命令和固定子命令候选。
- Terminal completer：`internal/console` terminal reader — 新增 `x/term.Terminal.AutoCompleteCallback` 挂接。
- Control console 兼容守护：`internal/control` client/server session — 保持旧 line mode，不新增 client-first 协议头。

### 2.4 推进策略

1. 命令元数据与纯补全函数：抽出静态 command spec，实现 `CompleteCommandLine`，覆盖所有顶层命令、唯一匹配、公共前缀、子命令、无匹配和普通文本。
   退出信号：不启动 console 也能用单元测试证明 `/login`、`/exit`、`/protection` 和 `/protection status` 等补全结果。
2. Console LineReader 接入：把 console loop 改为通过 LineReader 读行，保留 `RunWithIO` 的 buffered 行读取兼容入口。
   退出信号：现有 console 单元测试不需要 TTY 仍能通过。
3. 本地 TTY terminal reader：在直接 console 入口接入 `x/term.Terminal` 和 raw mode，Tab 调用同一补全函数。
   退出信号：TTY 冒烟中 `/log<Tab>`、`/pro<Tab>` 和 `/protection st<Tab>` 在 Enter 前补全。
4. Control console 兼容守护：保持 `webot-msg console` 旧 line mode，不发送协议头，并补 Attach 级测试防止 mixed-version 误输入。
   退出信号：fake old server 只收到用户输入；`printf '/exit\n' | webot-msg console` 仍能退出。
5. 文档和回归覆盖：更新用户文档、架构/需求落档，并补齐 control client/server 兼容性相关测试。
   退出信号：`go test ./...` 通过，文档明确 Tab 补全只面向交互式 TTY。

### 2.5 结构健康度与微重构

##### 评估

- 文件级 — `internal/console/console.go`：当前约 160 行，承担 help、prompt、读行、命令分发和 `/protection` 子命令处理。直接把 terminal raw mode 和补全算法塞进去会混合终端 IO 与命令语义。
- 文件级 — `internal/control/client.go`：只负责连接 socket 和双向复制；本轮不加入 raw mode 或协议头。
- 文件级 — `internal/control/server.go`：负责 socket accept、输出同步和 console session 挂接；本轮保持直接 `RunWithIO`。
- 目录级 — `internal/console`：当前只有 console 主文件和测试。新增 `commands` / `completion` / `line_reader` 文件符合目录职责，不需要重组目录。
- 目录级 — `internal/control`：当前已有 server/client 文件。本轮只补兼容性测试，不新增协议文件。
- compound convention 检索：`.codestable/compound` 当前没有可用 convention 文档。

##### 结论：不做前置微重构

原因：本次可以通过新增职责清晰的小文件承载 command spec、补全算法和本地 terminal reader，现有文件只做挂接，不需要先进行“只搬不改行为”的拆文件。实现阶段应避免把 `x/term` raw mode 细节写进命令分发函数，也不要在 control socket 里加入未协商协议。

##### 超出范围的观察

- `internal/console` 后续如果继续增加多级命令，可能需要把命令分发也收敛到 command registry；这会改变执行结构，建议另走 `cs-refactor` 或独立 feature，不阻塞本次固定补全能力。

## 3. 验收契约

关键场景清单：
- 输入：交互式 console 中键入 `/pro` 后按 Tab → 期望：当前行变为 `/protection `，不会执行命令。
- 输入：交互式 console 中键入 `/log` 后按 Tab → 期望：当前行变为 `/login`，不会执行命令。
- 输入：交互式 console 中键入 `/ex` 后按 Tab → 期望：当前行变为 `/exit`，不会退出；只有按 Enter 后才退出。
- 输入：交互式 console 中键入 `/protection st` 后按 Tab → 期望：当前行变为 `/protection status`，按 Enter 后才查询状态。
- 输入：交互式 console 中键入 `/b` 后按 Tab → 期望：最多补到 `/bot` 公共前缀，不自动加空格或选择 `/bots`。
- 输入：交互式 console 中键入 `/unknown` 后按 Tab → 期望：输入不变；按 Enter 后仍走现有未知 slash command 处理。
- 输入：交互式 console 中键入 `hello` 后按 Tab → 期望：不触发命令补全，不调用 `SendText`；只有按 Enter 后才按普通文本发送。
- 输入：`/protection ` 后按 Tab → 期望：如果没有进一步前缀，不改变输入或只展示候选，不自动选择 `enable` / `disable` / `status`。
- 输入：非 TTY 测试用 `bytes.Buffer` 传入 `/protection status\n/exit\n` → 期望：仍按行执行，测试不需要 terminal raw mode。
- 输入：新 `webot-msg console` 连接不支持 mode header 的旧 service，并传入 `/exit\n` → 期望：旧 service 只收到 `/exit\n`，不会收到任何 `WEBOT-MSG-CONSOLE` 协议文本。
- 输入：`printf '/exit\n' | webot-msg console` → 期望：仍能退出 control session；不要求 Tab 补全。
- 输入：直接前台 TTY 控制台按 Ctrl+C → 期望：终端 raw mode 被恢复，程序保存配置并退出，而不是只关闭 console 后继续后台运行。
- 输入：补全期间后台收到消息广播输出 → 期望：不会执行未完成输入；若终端重绘不完美，不能破坏最终 Enter 后提交的命令内容。
- 反向核对：本 feature 不新增配置项、auth store 字段、HTTP API 路由、Redis key 或 iLink 请求结构。

## 4. 与项目级架构文档的关系

本 feature 有系统级可见变化，acceptance 阶段需要提炼回 `.codestable/architecture/ARCHITECTURE.md`：
- 名词：补充 Terminal line reader / line mode fallback，说明 console 在 TTY 下支持按键级补全，非 TTY 仍按行读取。
- 动词骨架：补充直接前台 TTY 控制台的 raw mode / interrupt 处理；同时说明 control console 维持旧 line mode。
- 流程级约束：补充 Tab 补全只处理 command spec 中声明的静态 Console 命令和固定子命令，不读取凭据、不执行命令、不改变 active bot；非 TTY 输入保持兼容。

`.codestable/requirements/bot-message-bridge.md` 在验收阶段需要补充用户故事或变更记录：本地/控制台交互支持命令补全，降低运行中控制台操作成本。

用户文档可在 `docs/user/runtime-config.md` 或 Linux systemd 部署文档中轻量说明：直接前台运行的交互式 TTY 控制台支持 Tab 补全所有已声明 Console 命令和 `/protection` 子命令；`webot-msg console` 和脚本管道保持 line mode，不支持按键级补全。
