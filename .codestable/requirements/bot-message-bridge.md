---
doc_type: requirement
slug: bot-message-bridge
pitch: 在本地登录微信 bot 后，用控制台或受保护 API 回复最近会话，并可通过 TOML、可选发送保护和 Linux systemd 脚本管理本地运行
status: current
last_reviewed: 2026-06-11
implemented_by:
  - architecture-overview
tags: [bot, messaging, local-api, config, deploy, systemd, console, autocomplete, protection, redis]
---

# 本地登录微信 bot 并回复最近会话

## 用户故事

- 作为需要调试 bot 回复的人，我希望扫码登录后能直接在终端发送文本，而不是每次都手动拼远端请求。
- 作为要把 bot 接到自动化流程的人，我希望有一个本地 HTTP 入口发送消息，而不是让外部流程直接持有远端 bot 凭证。
- 作为同时维护多个 bot 的人，我希望能查看、切换和删除已登录 bot，而不是改本地配置文件。
- 作为本地部署或调试工具的人，我希望能用 TOML 调整 API 端口、auth store 路径、iLink BaseURL 和日志文件策略，而不是改代码或依赖工作目录里的固定路径。
- 作为用 systemd 部署服务的人，我希望能进入正在运行服务的控制台执行 `/login`，退出控制台时服务继续运行，而不是再启动一个新实例。
- 作为经常使用控制台命令的人，我希望能用 Tab 补全 `/login`、`/exit`、`/protection status` 这类命令，减少运行中服务操作时的输入错误。
- 作为在 Linux 机器上部署本工具的人，我希望能用脚本完成编译、默认配置落盘和 systemd service 安装/升级，而不是手动拼 service 文件和升级顺序。
- 作为依赖微信主动对话限制发送消息的人，我希望能从本地控制台可选开启、关闭并查看保护模式，在接近 10 次下发或 24h 主动对话窗口限制时收到提醒并冻结普通发送，避免工具继续误发。
- 作为开启发送保护模式的人，我希望每条普通文本在微信侧直接带上剩余可发条数和距离限制触发的剩余时间，避免频繁回到控制台查询 `/protection status`。

## 为什么需要

bot 登录态、消息上下文、发送入口、本地运行参数、微信侧发送限制和 Linux 部署动作分散处理时，很容易把凭证、会话上下文、调试动作、保护边界和部署差异混在一起。这个项目把它们收束到一个本地工具里，让开发者可以先登录 bot、等到消息上下文就绪，再从控制台或受保护 API 发出回复；运行参数和 Redis 连接通过独立 TOML 配置表达，Linux systemd 部署通过仓库脚本编排，不混入凭据文件。对需要长期自动化发送的场景，用户可以从控制台开启 Redis 支撑的保护模式，把微信侧“需要主动对话”的限制变成明确提醒和冻结，而不是让外部调用者盲目继续发送。

## 怎么解决

用户扫码登录后，工具在本地保存可复用的 bot 配置，持续接收消息并记录最近可回复的会话上下文。需要发送时，用户可以在控制台输入文本，也可以让外部流程调用本地受保护入口，由工具代为把文本回复到当前上下文。直接前台运行 service 的交互式 TTY 控制台支持 Tab 补全已声明的顶层命令和固定子命令，按 Ctrl+C 会保存配置并退出进程；systemd 部署下，用户通过 `webot-msg console` 连接运行中服务的本地 Unix socket，本地 stdin/stdout 都是 TTY 时同样支持 Tab 补全，退出控制台不会停止 service。管道、脚本和第三方 socket 客户端仍走 line mode。

启动时，工具默认读取 `~/.webot-msg/config/webot-msg.toml`，配置 API 端口、auth store 路径、本地控制台 socket、iLink BaseURL、日志文件路径、日志大小上限和 Redis 连接；默认配置不存在时回退内置默认值。默认 auth store、日志文件与 control socket 统一落在 `~/.webot-msg/` 下，auth store JSON schema 不变。

发送保护模式默认关闭，开启状态不写入 TOML，而是由服务进程写入 `~/.webot-msg/state/protection.json`。用户配置 Redis 后，在运行中控制台执行 `/protection enable` 开启；工具会在重启或升级后读取状态文件并尝试一次自动恢复保护。工具按 bot 记录最近主动对话后的普通文本和保护提醒下发次数，以及 24h 主动对话窗口。保护开启且普通文本预留成功时，工具会在用户请求正文末尾追加一行保护状态，显示本次发送计入后的剩余可发条数和距离限制触发的剩余时间；该状态来自同一次 Redis 预留，不需要外部调用者额外查询。接近次数限制或时间窗口限制时，工具会给当前上下文发送保护提醒并冻结普通文本发送；用户从微信 App 主动给机器人发一条消息后，工具解除冻结并重置该 bot 的计数和窗口。用户可通过 `/protection status` 查看当前 active bot 离触发限制还剩多少次数或时间，通过 `/protection disable` 关闭运行期保护。

在 Linux systemd 环境中，用户可以用 `scripts/linux-service.sh install` 编译 `bin/webot-msg`、安装 `/usr/local/bin/webot-msg`、创建 `~/.webot-msg/config/` 和 `~/.webot-msg/logs/`、首次写入默认 `webot-msg.toml`，并生成 `webot-msg.service`。升级时，脚本先按 `systemctl is-active` 记录服务是否运行：运行中则 stop、替换系统 PATH 中的二进制后再 start；未运行则只替换二进制。

## 边界

- 它只处理文本回复和 typing 状态，不负责富媒体消息编排。
- 它依赖最近消息提供可回复上下文；没有上下文时不能主动创建新会话。
- 它是本地运行工具，不提供多用户权限系统或公网部署安全边界。
- 它不替用户管理微信侧 bot 的生命周期，只保存本地登录和调用所需信息。
- Runtime config 不保存 bot token、API token、context token 或消息上下文；这些仍属于 auth store。
- 配置入口仅包含默认 `~/.webot-msg/config/webot-msg.toml`、兼容 `-c`、兼容 `-port` 和 `serve` / `console` 命令，不提供环境变量配置入口。
- `webot-msg console` 只连接本机 Unix socket，不提供公网远程管理通道。
- Tab 补全只覆盖本地 TTY 控制台里已声明的控制台命令和固定子命令，包括直接前台控制台和 `webot-msg console` interactive attach；不补全 bot 序号、botID、Redis 配置、文件路径、历史消息或普通文本。非 TTY 管道输入和 `nc` / `socat` 直连 control socket 不提供按键级补全。
- 日志配置只提供文件路径和大小上限，不提供按时间切割、压缩归档、保留天数或远程日志采集。
- 默认 auth store 按本地凭据处理，目录和文件使用 owner-only 权限；显式自定义路径的挂载、备份和系统权限由部署者负责。
- Linux 部署脚本只面向 systemd 单实例，不提供 `.deb`、RPM、Docker、Ansible、多实例管理、备份或回滚。
- 安装脚本不会覆盖已有 `~/.webot-msg/config/webot-msg.toml`，也不会删除或修改 `~/.webot-msg/config/auth.json`；服务操作需要部署者具备 sudo 权限。
- 安装脚本固定把用户可执行命令安装到 `/usr/local/bin/webot-msg`，不提供安装前缀配置。
- 发送保护模式初始默认关闭；成功执行 `/protection enable` 后，保护开关状态持久化到 `~/.webot-msg/state/protection.json`，服务重启或升级后只尝试一次自动恢复。恢复失败不阻止服务启动，也不做后台重试。
- 执行 `/protection enable` 依赖 Redis 配置可用；启用成功后 Redis 不可用时普通文本发送会 fail closed。
- 保护模式不绕过微信限制，也不模拟用户主动对话；解除冻结只能依赖微信 App 主动消息被监听到。
- 保护模式按 bot 计数，不支持同一 bot 下多个会话分别计数；仍沿用最近一个 `IlinkUserID` / `ContextToken` 的会话模型。
- `/bots/{botID}/typing` 不计入保护模式的下发次数，冻结状态也不阻止 typing。
- 保护模式开启时，HTTP API 和控制台的普通文本真实发送正文会追加保护状态行；HTTP 请求 / 响应 JSON 不新增字段，也不回显追加后的正文。保护提醒消息和 `/bots/{botID}/typing` 不追加状态行。
- Redis URL、Redis password 和 Redis 保护状态不写入 auth store；保护开关状态只写入 `~/.webot-msg/state/protection.json`。真实 `redis.password` 属于本地部署凭据，不应提交。

## 变更记录

- 2026-06-10：新增 TOML Runtime config 能力，覆盖 API 端口、auth store 路径、iLink BaseURL、日志文件路径和大小上限；默认存储迁移到 `~/.webot-msg/`，并保留旧 `./config/auth.json` 到新默认路径的一次性复制兼容。
- 2026-06-10：新增 Linux systemd 部署脚本，支持 `install`、`upgrade` 和 `start` / `stop` / `restart` / `status` 服务控制；安装首次写入默认 Runtime config，升级只恢复原本 active 的服务。
- 2026-06-10：新增 `webot-msg console` 本地控制台入口，通过 Unix socket 进入 systemd 管理的运行中服务；`/exit` 和 `/quit` 只退出控制台连接，停止进程仍由 systemd 管理。
- 2026-06-10：服务和控制台默认读取 `~/.webot-msg/config/webot-msg.toml`，同时保留 `-c` 兼容入口，避免破坏旧脚本。
- 2026-06-10：Linux 部署脚本新增把二进制安装到 `/usr/local/bin/webot-msg`，让 `which webot-msg` 和 `webot-msg console` 在部署后直接可用。
- 2026-06-10：新增默认关闭的微信发送保护模式；开启后用 Redis 按 bot 记录下发计数和 24h 主动对话窗口，在临界时发送保护提醒、冻结普通文本发送，并在微信 App 主动消息到达后重置。
- 2026-06-10：发送保护开启状态从 Runtime config 移到运行中控制台命令；TOML 只保留 Redis 配置，新增 `/protection enable`、`/protection disable` 和 `/protection status`。
- 2026-06-11：直接前台 TTY 控制台新增 Tab 补全，覆盖 `/login`、`/exit`、`/protection` 等已声明命令和 `/protection enable|disable|status` 固定子命令。
- 2026-06-11：发送保护开关状态持久化到 `~/.webot-msg/state/protection.json`，服务重启或升级后尝试一次自动恢复，`/protection disable` 会写入关闭状态。
- 2026-06-11：`webot-msg console` 在本地 stdin/stdout 都是 TTY 时新增客户端行编辑和 Tab 补全；非 TTY 管道输入、第三方 socket 客户端和 socket 上行字节流契约保持行模式兼容。
- 2026-06-11：保护模式开启时，HTTP API 和控制台普通文本末尾追加保护状态行，显示本次发送计入后的剩余可发条数和距离限制触发的剩余时间；状态来自同一次 Redis 预留快照。
