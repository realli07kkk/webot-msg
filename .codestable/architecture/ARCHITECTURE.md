---
doc_type: architecture
slug: architecture-overview
scope: webot-msg 当前 CLI/API 服务整体结构
summary: CLI 启动一个本地应用，由 app 编排配置、iLink 客户端、HTTP API、带 Tab 补全的控制台、每个 bot 的消息监听、可自动恢复的 Redis 发送保护和 Linux systemd 部署脚本。
status: current
last_reviewed: 2026-06-12
tags: [go, cli, api, bot, config, logging, deploy, systemd, console, autocomplete, protection, redis, state]
depends_on: []
implements:
  - bot-message-bridge
---

# webot-msg 架构总入口

## 0. 术语

- Bot：本文指一个已扫码登录的微信 bot 配置项，对应 `config.UserConfig`，包含 bot token、bot id、更新游标、最近消息上下文和本地 API token。代码锚点：`internal/config/store.go:21`。
- Active bot：单个控制台会话当前选中的发送身份，保存在 `internal/console` 的 session-local 变量里；多个 control console 连接互不共享该状态。代码锚点：`internal/console/console.go:46`。
- 消息上下文：发送回复需要的 `IlinkUserID` 与 `ContextToken`，由监听更新时写回本地配置。代码锚点：`internal/app/app.go:379`。
- Runtime config：启动时读取的 TOML 配置，控制 API 端口、auth store 路径、本地控制台 socket、iLink BaseURL、日志文件策略和 Redis 连接，不保存 bot 凭据、消息上下文或保护开关值；保护开关状态文件路径由 runtimeconfig 内置默认值解析，不暴露为 TOML key。代码锚点：`internal/runtimeconfig/config.go:35`。
- 前台 console：无参运行 `webot-msg` 且 stdin/stdout 是 TTY 时，service 启动完整监听、HTTP API、保护和 control socket 后进入的本地交互控制台；按 Ctrl+C 会保存配置并退出进程。代码锚点：`cmd/webot-msg/main.go:15`、`internal/app/app.go:150`。
- Control console：service 通过 Unix socket 暴露的交互控制台通道，复用 `internal/console` 命令语义；可用 `nc -U` / `socat` 等本地 socket 客户端连接，`/exit` 和 `/quit` 只关闭当前连接，不停止 service。代码锚点：`internal/control/server.go:41`。
- Console 命令补全：交互式 TTY 控制台按 Tab 时补齐 `internal/console` 静态命令表里的顶层命令和固定子命令；补全不执行命令、不读取凭据、不改变 active bot。非 TTY 输入走 line mode fallback。代码锚点：`internal/console/completion.go:8`、`internal/console/terminal_reader.go:20`。
- Control client 休眠库：`internal/control` 仍保留 `Attach` / `AttachInteractive` / `outputSplitter` 等 client 端 helper 及测试，但当前可执行入口不再挂载第一方 attach 命令；运行中服务的实际控制入口是 socket server。代码锚点：`internal/control/client.go:9`、`internal/control/interactive_client.go:26`、`internal/control/output_splitter.go:15`。
- Control console line mode：`nc -U` / `socat` 等第三方客户端直连 control socket 时保持字节流行模式，不发送协议头、不切 raw mode、不提供按键级 Tab 补全。代码锚点：`internal/control/server.go:57`。
- Log file policy：标准日志的文件输出路径和大小上限，默认写入 `~/.webot-msg/logs/webot-msg.log`，达到上限后只保留一个 `.1` 备份。代码锚点：`internal/logfile/writer.go:9`。
- 保护模式：初始默认关闭、由本地控制台 `/protection enable|disable|status` 运行期控制的发送保护能力；开启后，普通文本发送先经过本地保护编排，Redis 判断即将触发微信下发次数或 24h 主动对话限制时会发送提醒并冻结普通发送。保护开启且普通文本预留成功时，发送正文末尾会追加一行保护状态行，展示本次发送计入后的剩余可发条数和距离限制触发的剩余时间。保护开关状态会落到本地状态文件，服务启动时可尝试一次自动恢复。代码锚点：`internal/console/console.go:133`、`internal/protection/runtime_guard.go:16`、`internal/sender/protected_text.go:22`、`internal/sender/status_footer.go:12`。
- 预留快照：`ReserveNormalSend` 通过 Redis Lua 脚本原子完成普通文本发送预留，并在 `send` / `send_then_reminder` 路径随 `Reservation` 返回 `MessagesBeforeReminder` 和 `TimeBeforeWarning`；保护状态行只消费这个快照，不做预留后的二次 Redis 查询。代码锚点：`internal/protection/guard.go:103`、`internal/protection/redis_guard.go:210`。
- 保护开关状态文件：`~/.webot-msg/state/protection.json`，由 service 进程在 `/protection enable` 成功后写入开启、在 `/protection disable` 后写入关闭，只保存 `protection_enabled` 布尔值；文件格式由 `internal/protection.StateStore` 管理。代码锚点：`internal/protection/state_store.go:16`、`internal/protection/state_store.go:28`、`internal/protection/state_store.go:44`。
- Redis 保护状态：保护模式开启时按 bot 存在 Redis 中的状态，包含下发计数、冻结原因、提醒状态和主动对话 TTL；规则全局共用，状态按 botID 隔离。代码锚点：`internal/protection/redis_guard.go:94`、`internal/protection/redis_guard.go:156`。
- 冻结状态：保护模式已进入提醒 / 拒绝阶段后，HTTP API 和控制台普通文本发送都会被拒绝，直到下一次微信 App 主动消息触发 `RecordActiveConversation` 重置。代码锚点：`internal/api/server.go:121`、`internal/app/app.go:408`。
- Linux deploy script：仓库内的 Linux/systemd 编译部署入口，负责编译 `bin/webot-msg`、安装 `/usr/local/bin/webot-msg`、首次写入默认 Runtime config、安装 `webot-msg.service`、升级时按原运行状态 stop/start。代码锚点：`scripts/linux-service.sh:1`。
- Service unit：Linux deploy script 生成的 systemd unit，固定名为 `webot-msg.service`，用 `ExecStart=/usr/local/bin/webot-msg` 启动服务；程序固定读取运行用户的 `~/.webot-msg/config/webot-msg.toml`。代码锚点：`scripts/linux-service.sh:206`。

## 1. 定位与受众

本项目是一个 Go 写的本地 CLI/API 服务，用来扫码登录微信 bot、监听消息上下文，并从控制台或 HTTP API 发送文本回复。systemd 部署时，控制台通过本地 Unix socket 进入运行中的 service，而不是再启动第二个实例。读者主要是做 feature-design、issue-analyze 或新接手项目的人；读完应能定位入口、状态归属、外部调用边界和凭证风险。

## 2. 结构与交互

`cmd/webot-msg/main.go` 是唯一可执行入口，只接受无参运行。带任何参数都会向 stderr 打印一行错误并以退出码 2 结束；无参时固定读取 `~/.webot-msg/config/webot-msg.toml`，默认文件缺失时回退内置默认值，加载并校验 Runtime config，准备本地存储目录和文件日志，然后用解析后的 auth store path、iLink BaseURL 与 control socket path 创建应用。API 端口只来自 TOML 或内置默认值，不再有 CLI 覆盖入口。代码锚点：`cmd/webot-msg/main.go:15`、`cmd/webot-msg/main.go:91`。

`internal/runtimeconfig` 是启动配置计算层。它先给出内置默认值，再按可选 TOML 覆盖，最后做 `~` 展开、端口范围、BaseURL scheme、control socket path、日志大小和未知 key 校验。默认存储根目录是 `~/.webot-msg/`，默认 auth store、日志路径、保护开关状态文件和控制台 socket 分别落在 `config/`、`logs/`、`state/` 和根目录；TOML 只保留 Redis 连接配置，不保存保护开关值，也不暴露状态文件路径 key。旧 `[protection]` section 只作兼容解析，不再驱动运行行为。代码锚点：`internal/runtimeconfig/config.go:17`、`internal/runtimeconfig/config.go:35`、`internal/runtimeconfig/config.go:213`。

`scripts/linux-service.sh` 是 Linux/systemd 部署编排入口。`install` 会检查 Linux/systemd、Go、部署用户 home 和 sudo 权限，编译当前源码到 `bin/webot-msg`，再安装到 `/usr/local/bin/webot-msg`，创建部署用户的 `~/.webot-msg/config/` 与 `~/.webot-msg/logs/`，首次写入默认 `webot-msg.toml`，再生成 `/etc/systemd/system/webot-msg.service` 并执行 `systemctl daemon-reload`。`upgrade` 会先用 `systemctl is-active --quiet webot-msg` 记录服务是否 active；active 时先 stop，替换系统 PATH 中的二进制、刷新 systemd unit 并 `daemon-reload` 后再 start；非 active 时只替换二进制并刷新 unit，不主动启动；已有配置缺少 `[redis]` 时会追加默认 Redis section，不覆盖已有字段。`start`、`stop`、`restart`、`status` 子命令透传到 `systemctl`。代码锚点：`scripts/linux-service.sh:261`、`scripts/linux-service.sh:272`、`scripts/linux-service.sh:300`。

`internal/logfile` 是标准日志文件输出的轻量大小控制层。传入空日志路径时禁用文件日志；传入路径时以追加方式打开文件，并在下一次写入会超过上限时把当前文件轮转为 `.1`。代码锚点：`internal/logfile/writer.go:17`、`internal/logfile/writer.go:47`、`internal/logfile/writer.go:74`。

`internal/app` 是编排层，持有配置仓库、iLink 客户端、control socket path、运行期保护 guard、保护开关状态 store 和正在运行的监听 / 保护检查协程集合。它负责启动时加载配置、读取保护开关状态并在记录为开启时尝试一次恢复、交互终端下必要时扫码登录、补齐 API token、启动 control console、启动监听、启动 HTTP API，再进入控制台循环；非交互 stdin 下不会自动扫码阻塞 service。控制台 `/protection enable` 会按 Runtime config 中的 Redis 配置创建 Redis guard、为已登录 bot 启动时间窗口检查器并写入开启状态，`/protection disable` 会切回 no-op guard、取消检查器并写入关闭状态，`/protection status` 查询当前 active bot 的剩余额度。代码锚点：`internal/app/app.go:34`、`internal/app/app.go:97`、`internal/app/app.go:216`、`internal/app/app.go:234`。

`internal/sender` 是普通文本发送编排层。它把“Redis 保护预留、按预留快照追加保护状态行、iLink 普通文本发送、发送失败释放预留、必要时发送保护提醒并记录提醒”收敛成一个入口，供 HTTP API 和控制台共同调用；保护关闭或快照缺失时正文保持原样，保护提醒不递归走普通文本 guard，也不追加状态行。代码锚点：`internal/sender/protected_text.go:22`、`internal/sender/protected_text.go:47`、`internal/sender/protected_text.go:80`、`internal/sender/status_footer.go:12`。

`internal/protection` 是保护状态计算层。`RuntimeGuard` 是进程内运行期开关，默认让新操作走 `NoopGuard` 保持原行为，启用后让新操作绑定当前 Redis guard generation；运行期 disable 只阻止新保护操作，已开始的保护发送事务继续使用同一 generation 完成 release/record 后再关闭 Redis client。`StateStore` 管理本地保护开关状态文件，文件不存在视为关闭，非法 JSON 返回错误由 app 告警后继续，保存使用临时文件和 rename 原子替换。Redis 实现使用 `github.com/redis/go-redis/v9`，通过 Lua 脚本原子处理普通发送预留、预留释放、提醒记录、主动对话重置和 24h TTL 检查，并提供只读状态查询给控制台 status；普通发送预留成功时，同一次 Lua 调用还会返回本次发送计入后的剩余额度和 active TTL 快照给 sender 拼装状态行。代码锚点：`internal/protection/runtime_guard.go:10`、`internal/protection/state_store.go:20`、`internal/protection/redis_guard.go:29`、`internal/protection/redis_guard.go:64`、`internal/protection/redis_guard.go:210`。

`internal/api` 暴露 `/bots/{botID}/messages` 和 `/bots/{botID}/typing` 两类动作。请求先从 `Authorization: Bearer` 或参数里取 token，再按 bot id 查本地配置并校验 `APIToken`。普通文本发送通过 `internal/sender` 进入保护编排；typing 仍直接调用 iLink 客户端，不计入保护计数，冻结状态下也不阻止。代码锚点：`internal/api/server.go:22`、`internal/api/server.go:109`、`internal/api/server.go:129`。

`internal/console` 只依赖 `Controller` 接口，负责 `/login`、`/bots`、`/bot <num>`、`/del <num>`、`/protection enable|disable|status`、`/exit`、`/quit` 和普通文本发送。控制台的命令 help 与 Tab 补全候选来自同一份静态 `CommandSpec`；TTY 下使用 `TerminalLineReader` 和 `golang.org/x/term.Terminal.AutoCompleteCallback` 补全已声明命令/固定子命令，非 TTY 和测试通过 `BufferedLineReader` 保持按行读取。active bot 是会话本地状态，不写回 `app` 全局。代码锚点：`internal/console/console.go:20`、`internal/console/commands.go:3`、`internal/console/terminal_reader.go:19`。

`internal/control` 提供运行中 service 的本地控制台通道。server 在配置的 Unix socket path 监听并清理 stale socket，把每个连接交给 `console.RunWithIO`；server 侧不感知 client 是否 TTY。当前可执行入口不再挂载第一方 attach 命令，systemd 场景使用 `nc -U` / `socat` 等本地 socket 客户端直接连接 control socket，按普通 line mode 输入命令，不发送协议头、不提供按键级 Tab 补全。`Attach`、`AttachInteractive` 和 output splitter 仍作为休眠库保留，以便未来恢复 attach 能力时复用。代码锚点：`internal/control/server.go:17`、`internal/control/server.go:57`、`internal/control/client.go:9`、`internal/control/interactive_client.go:41`、`internal/control/output_splitter.go:15`。

`internal/ilink` 是外部 HTTP API 适配层，封装 QR 登录、拉取更新、发送消息、发送 typing 状态和 bot 配置读取。所有远端请求都通过 `Client.BaseURL` 组装 endpoint。代码锚点：`internal/ilink/client.go:21`、`internal/ilink/client.go:57`、`internal/ilink/client.go:131`、`internal/ilink/client.go:174`。

## 3. 数据与状态

持久化入口是 `config.Store`。auth store 的 JSON schema 不变，仍保存 bot token、API token、更新游标和消息上下文；默认运行时路径从旧的 `./config/auth.json` 迁到 `~/.webot-msg/config/auth.json`。Runtime config 是独立 TOML，不进入 auth store；control socket 是运行时 IPC 文件，不保存业务状态。仓库用互斥锁保护内存中的 `AppConfig`，读写 bot 列表、token、更新游标和消息上下文时都通过 Store 方法。代码锚点：`internal/config/store.go:14`、`internal/config/store.go:40`、`internal/runtimeconfig/config.go:20`。

保护开关状态持久化在 `~/.webot-msg/state/protection.json`，不写入 Runtime config、auth store 或 Redis。该文件只保存 `protection_enabled` 布尔值；service 启动时读取它，记录为开启时尝试一次运行期 enable，成功后保护开启，失败则记录告警并保持关闭且不改写文件。保护计数和冻结状态的 source of truth 仍是 Redis。每个 bot 使用 `{prefix}:protect:{<botID>}:state` Hash 保存 `out_count`、`frozen`、`reason`、`reminder_pending`、`reminder_sent_ms` 和 `last_active_ms`，使用 `{prefix}:protect:{<botID>}:active` String 的 TTL 表达 24h 主动对话窗口；key 中的 `{botID}` hash tag 让同一 bot 的多 key Lua 操作在 Redis Cluster 下落同一 slot。代码锚点：`internal/app/app.go:257`、`internal/protection/state_store.go:16`、`internal/protection/redis_guard.go:117`、`internal/protection/redis_guard.go:190`。

`UserConfig` 是核心持久化结构。`BotToken` 用于调用 iLink，`APIToken` 用于保护本地 HTTP API，`GetUpdatesBuf` 是拉取更新的游标，`IlinkUserID` 与 `ContextToken` 是回复最近会话的上下文。代码锚点：`internal/config/store.go:21`。

监听状态按 bot 分协程运行，`runningMonitors` 防止同一个 bot 重复启动监听。每次拉取更新后，app 将更新游标和新的 context token 写回 Store，再打印消息内容；如果更新中包含带 `from_user_id` 和 `context_token` 的主动消息，会同步重置该 bot 的 Redis 保护状态和 24h TTL。代码锚点：`internal/app/app.go:268`、`internal/app/app.go:379`、`internal/app/app.go:408`。

兼容迁移只在使用默认 auth store path 时发生：如果旧 `./config/auth.json` 存在且新 `~/.webot-msg/config/auth.json` 不存在，启动前原样复制一次；如果新文件已存在，旧文件不会覆盖新文件。复制目标文件按 owner-only 权限创建。代码锚点：`internal/runtimeconfig/config.go:248`、`internal/runtimeconfig/config.go:277`。

Linux deploy script 首次安装时会写入默认 Runtime config 文件 `~/.webot-msg/config/webot-msg.toml`，内容与 Runtime config 默认契约一致；已有 TOML 时不会覆盖既有字段，升级只在缺少 `[redis]` section 时追加默认 Redis 配置；`~/.webot-msg/config/auth.json` 仍由 `config.Store` 管理，不由部署脚本迁移、覆盖或删除。保护开关状态文件由 service 运行时创建，部署脚本不直接写入。代码锚点：`scripts/linux-service.sh:167`。

## 4. 关键决策

- Runtime config 与 auth store 分离：启动参数使用 TOML，运行态凭据、游标和上下文继续放在 auth store JSON 中，避免把可提交的启动配置和本地凭据混在一起。
- 默认本地存储统一收敛到 `~/.webot-msg/`：固定 Runtime config 路径是 `~/.webot-msg/config/webot-msg.toml`，默认 auth store 使用 `~/.webot-msg/config/auth.json`，默认标准日志使用 `~/.webot-msg/logs/webot-msg.log`，默认保护开关状态文件使用 `~/.webot-msg/state/protection.json`，默认 control socket 使用 `~/.webot-msg/webot-msg.sock`；TOML 内部的 auth / log / socket 路径配置仍可指向其他位置，保护开关状态文件路径固定不暴露配置项。
- 配置入口保持克制：本项目固定读取 `~/.webot-msg/config/webot-msg.toml`，不提供 CLI flag、子命令或环境变量配置入口；API 端口只能通过 TOML `api.port` 或内置默认值决定。
- systemd 交互通过本地 Unix socket，不尝试 attach systemd service 的 stdin，也不通过再启动第二个服务实例进入控制台；当前没有第一方 attach 命令，使用 `nc -U` / `socat` 等本地 socket 客户端连接。
- 控制台补全只来源于静态命令表：Tab 补全覆盖已声明顶层命令和固定子命令，不补全 bot 序号、botID、文件路径、Redis 配置、历史消息或普通文本；补全本身不触发 Controller 调用。
- auth store 权限按凭据处理：新建 auth 目录使用 owner-only，auth 文件保存和 legacy copy 后都保持 owner-only；日志文件不使用 auth store 权限策略。
- Linux 部署入口保持仓库脚本形态：本项目提供 Bash 脚本管理单个 `webot-msg.service`，不引入 `.deb`、RPM、Docker、Ansible 或多实例管理。
- 部署后二进制进入系统 PATH：脚本保留仓库 `bin/webot-msg` 作为构建产物，同时安装 `/usr/local/bin/webot-msg` 作为用户命令和 systemd `ExecStart`，避免部署后必须进入仓库目录执行控制台。
- 升级保持原运行意图并刷新 unit：`upgrade` 只在服务升级前处于 active 时 stop 后再 start；服务原本非 active 时只替换二进制和刷新 systemd unit，不主动启动。
- 保护模式初始默认关闭且可卸载：关闭时普通文本发送走 `NoopGuard`，保持既有 API / 控制台行为；只有执行 `/protection enable` 成功或启动恢复成功后进程才创建 Redis client 并启动保护检查器。恢复失败只告警并保持关闭，不做后台重试。
- 保护状态外置 Redis：发送限制、冻结和 24h 主动对话窗口不进入 auth store；Redis 不可用时保护开启路径 fail closed，拒绝普通文本发送。
- 普通文本发送前原子预留额度：保护开启时必须先通过 Redis Lua 脚本预留普通发送额度，避免并发请求打穿微信下发限制；iLink 普通文本发送失败时释放预留。
- 保护提醒也算下发消息：提醒真实调用 iLink `sendmessage`，发送成功后必须写入 `out_count`，否则系统计数会比微信侧少。
- 受保护发送事务绑定同一 guard generation：`ReserveNormalSend -> iLink SendMessage -> ReleaseNormalSend/RecordReminderSend` 不能被运行期 disable 切成不同 delegate；disable 只能影响新事务。
- 保护状态行只在保护开启且普通文本预留成功时追加到真实发送正文；HTTP API 请求 / 响应 JSON 契约保持不变，`/bots/{botID}/typing` 和保护提醒消息不追加状态行。状态行数据必须来自同一次 Redis 预留 Lua 返回的快照，禁止在 sender 中用 `ProtectionStatus` 做预留后的二次查询。代码锚点：`internal/sender/protected_text.go:47`、`internal/sender/status_footer.go:12`、`internal/protection/redis_guard.go:210`。

## 5. 代码锚点

- `cmd/webot-msg/main.go:main` — 唯一无参 CLI 入口，负责拒绝参数、加载 Runtime config 和启动 app。
- `internal/runtimeconfig/config.go:Config` — TOML Runtime config 的结构、默认值、校验和存储准备。
- `internal/logfile/writer.go:SizeWriter` — 标准日志文件输出和简单大小轮转。
- `internal/app/app.go:App.Run` — 启动编排主流程。
- `internal/sender/protected_text.go:SendProtectedText` — 普通文本发送的保护编排入口。
- `internal/protection/guard.go:Guard` — 保护服务接口和 no-op 实现。
- `internal/protection/state_store.go:StateStore` — 保护开关状态文件读写，负责缺失 / 损坏语义和原子保存。
- `internal/protection/redis_guard.go:RedisGuard` — Redis 保护状态、per-bot key 和 Lua 状态机。
- `internal/control/server.go:Server` — 运行中 service 的 Unix socket 控制台 server。
- `internal/control/client.go:Attach` — 休眠的 line mode 控制台连接 helper，当前可执行入口不调用。
- `internal/control/interactive_client.go:AttachInteractive` — 休眠的客户端行编辑控制台连接 helper，当前可执行入口不调用。
- `internal/control/output_splitter.go:outputSplitter` — 休眠客户端行编辑 helper 使用的 server 字节流切分状态机。
- `internal/app/app.go:monitorWeixin` — 每个 bot 的长轮询监听循环。
- `internal/api/server.go:handleBotAction` — HTTP API 鉴权和动作分发。
- `internal/config/store.go:Store` — 本地配置持久化和并发保护。
- `internal/console/console.go:Run` — 交互式控制台命令循环。
- `internal/console/commands.go:CommandSpec` — 控制台命令 help 和 Tab 补全候选源。
- `internal/console/completion.go:CompleteCommandLine` — 控制台命令 / 固定子命令补全计算。
- `internal/console/terminal_reader.go:TerminalLineReader` — 基于 `golang.org/x/term` 的 TTY 行读取和 Tab 补全接入。
- `internal/ilink/client.go:Client` — iLink HTTP 调用封装。
- `scripts/linux-service.sh:cmd_install` — Linux/systemd 安装编排，构建二进制、写默认配置和安装 service。
- `scripts/linux-service.sh:cmd_upgrade` — Linux/systemd 升级编排，按服务原 active 状态 stop/start。

## 6. 已知约束 / 边界情况

- auth store 包含 bot token、API token 和消息上下文，不能提交到 Git；默认运行时路径是 `~/.webot-msg/config/auth.json`，旧 `./config/auth.json` 只作为一次性兼容复制源。来源：`.gitignore`、`internal/config/store.go:14`、`internal/runtimeconfig/config.go:20`。
- 默认 Runtime config 文件 `~/.webot-msg/config/webot-msg.toml` 不存在时回退内置默认值；CLI 不接受配置路径或端口覆盖参数。代码锚点：`cmd/webot-msg/main.go:15`、`cmd/webot-msg/main.go:99`。
- Runtime config 使用严格 TOML 模式，未知 section / key 会启动失败；`ilink.base_url` 只接受 `http` 或 `https` scheme。代码锚点：`internal/runtimeconfig/config.go:129`、`internal/runtimeconfig/config.go:376`。
- 执行 `/protection enable` 时要求 Redis 可连接；`redis.url` 只接受 `redis` / `rediss` scheme，`redis.password` 和 URL userinfo password 不能同时配置，URL parse 错误不会回显原始 userinfo。启用失败不会把系统切到半开启状态。代码锚点：`internal/protection/redis_guard.go:29`、`internal/protection/redis_guard.go:43`、`internal/protection/runtime_guard.go:31`。
- 保护开关启动恢复只尝试一次：状态文件记录开启但 Redis 不可用时，服务继续启动、保护保持关闭、状态文件不改写，并提示用户修复 Redis 后手动 `/protection enable`；Redis 随后恢复不会自动开启保护。代码锚点：`internal/app/app.go:257`。
- 默认存储路径统一在 `~/.webot-msg/` 下；默认 auth store 目录和 auth 文件按 owner-only 权限处理，保护开关状态目录和文件也按 owner-only 权限处理，control socket 文件按 owner-only 权限创建。代码锚点：`internal/runtimeconfig/config.go:20`、`internal/protection/state_store.go:11`、`internal/config/store.go:17`、`internal/control/server.go:129`。
- 文件日志只接管标准库 `log` 输出，不接管控制台提示、二维码或收到的消息内容；启动摘要不能记录 bot token、API token、context token 或完整消息正文。代码锚点：`cmd/webot-msg/main.go:44`、`cmd/webot-msg/main.go:51`。
- 发送文本依赖最近消息上下文；如果当前会话选择的 bot 没有 `IlinkUserID` 或 `ContextToken`，控制台和 API 都会拒绝发送。代码锚点：`internal/app/app.go:196`、`internal/api/server.go:104`。
- 保护模式沿用每个 bot 只保存最近一个 `IlinkUserID` / `ContextToken` 的会话模型，不支持多会话独立计数；初次开启但 Redis 缺少主动对话窗口时会冻结并要求先从微信 App 主动发消息初始化。代码锚点：`internal/app/app.go:393`、`internal/protection/redis_guard.go:167`。
- `/protection status` 只展示当前控制台会话的 active bot；无 active bot 时只展示整体保护开关状态并提示先选择 bot。代码锚点：`internal/console/console.go:166`、`internal/app/app.go:228`。
- 冻结期间 HTTP API 普通文本发送返回 `429`，控制台普通文本发送返回保护锁定错误；`/bots/{botID}/typing` 不计入下发限制，也不受冻结状态影响。代码锚点：`internal/api/server.go:121`、`internal/app/app.go:202`、`internal/api/server.go:129`。
- HTTP API token 为空或不匹配都会返回 unauthorized，不能绕过本地 `APIToken` 校验。代码锚点：`internal/api/server.go:83`。
- 控制台 `/exit` 或 `/quit` 只关闭当前控制台会话，不停止 service；直接前台 TTY 控制台下按 Ctrl+C 会恢复终端、保存配置并退出进程；systemd 部署下停止进程仍用 `systemctl stop webot-msg` 或部署脚本 `stop`。stdin 关闭不是主动退出，服务会继续后台运行。代码锚点：`internal/console/console.go:62`、`internal/console/console.go:74`、`internal/app/app.go:138`。
- Tab 补全只在本地 stdin/stdout 都是 TTY 的前台 console 下工作；非 TTY stdin 和手工使用 `nc -U` / `socat` 直连 control socket 都走 line mode，不支持按键级补全。代码锚点：`internal/console/terminal_reader.go:20`、`internal/control/server.go:57`。
- Linux deploy script 只面向 Linux/systemd 单实例部署；它会拒绝非 Linux 或未运行 systemd 的环境。代码锚点：`scripts/linux-service.sh:47`。
- `webot-msg.service` 使用部署用户运行，`ExecStart` 指向 `/usr/local/bin/webot-msg`；脚本拒绝写入包含空白字符的 systemd 路径，避免 unit 解析歧义。代码锚点：`scripts/linux-service.sh:116`、`scripts/linux-service.sh:206`。
- 部署脚本不会覆盖已有 `~/.webot-msg/config/webot-msg.toml` 的既有字段或删除 `~/.webot-msg/config/auth.json`；升级可能追加缺失的 `[redis]` section。真实 Linux systemd 主机上的服务操作仍需要部署者具备 sudo 权限。代码锚点：`scripts/linux-service.sh:65`、`scripts/linux-service.sh:167`。

## 7. 相关文档

- `.codestable/requirements/bot-message-bridge.md` — 当前用户可感能力描述。
- `docs/user/linux-systemd-deploy.md` — Linux systemd 安装、升级和服务控制说明。
- `.codestable/requirements/VISION.md` — requirement 索引。
- `.codestable/attention.md` — CodeStable 技能启动必读的项目注意事项入口。
