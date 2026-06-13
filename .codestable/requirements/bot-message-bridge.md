---
doc_type: requirement
slug: bot-message-bridge
pitch: 在本地登录微信 bot 后，用控制台或受保护 API 回复最近会话，并可通过 TOML、可选发送保护和 Linux systemd 脚本管理本地运行
status: current
last_reviewed: 2026-06-13
implemented_by:
  - architecture-overview
tags: [bot, messaging, local-api, config, deploy, systemd, console, autocomplete, protection, redis, audit, uuid]
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
- 作为需要追溯每条机器人发出消息的人，我希望每条普通文本在微信侧正文最底部都带一个唯一 ID，方便把微信侧消息和本地记录对应起来。
- 作为有审计或合规需求的人，我希望能在运行中控制台可选开启、关闭并查看发送审计，开启后每条普通文本的发送时间和完整正文按这个 ID 落到 Redis 并带可配置的过期时间，而不是自己去抓发送日志。

## 为什么需要

bot 登录态、消息上下文、发送入口、本地运行参数、微信侧发送限制和 Linux 部署动作分散处理时，很容易把凭证、会话上下文、调试动作、保护边界和部署差异混在一起。这个项目把它们收束到一个本地工具里，让开发者可以先登录 bot、等到消息上下文就绪，再从控制台或受保护 API 发出回复；运行参数和 Redis 连接通过独立 TOML 配置表达，Linux systemd 部署通过仓库脚本编排，不混入凭据文件。对需要长期自动化发送的场景，用户可以从控制台开启 Redis 支撑的保护模式，把微信侧“需要主动对话”的限制变成明确提醒和冻结，而不是让外部调用者盲目继续发送。

## 怎么解决

用户扫码登录后，工具在本地保存可复用的 bot 配置，持续接收消息并记录最近可回复的会话上下文。需要发送时，用户可以在控制台输入文本，也可以让外部流程调用本地受保护入口，由工具代为把文本回复到当前上下文。无参运行 `webot-msg` 时，工具会先读取默认 TOML 并尝试连接配置里的 control socket：已有运行中服务时直接接入该服务的控制台，没有可用 socket 时才启动新的前台 service。直接前台 service 和 TTY 下的第一方 socket attach 都支持 Tab 补全已声明的顶层命令和固定子命令；接入已有服务时退出这次控制台连接不会停止 service。管道、脚本和第三方 socket 客户端都走 line mode。

启动时，工具固定读取 `~/.webot-msg/config/webot-msg.toml`，配置 API 端口、auth store 路径、本地控制台 socket、iLink BaseURL、日志文件路径、日志大小上限和 Redis 连接；默认配置不存在时回退内置默认值。默认 auth store、日志文件与 control socket 统一落在 `~/.webot-msg/` 下，auth store JSON schema 不变。CLI 入口只接受无参运行，端口和路径调整都通过默认 TOML 生效。

发送保护模式默认关闭，开启状态不写入 TOML，而是由服务进程写入 `~/.webot-msg/state/protection.json`。用户配置 Redis 后，在运行中控制台执行 `/protection enable` 开启；工具会在重启或升级后读取状态文件并尝试一次自动恢复保护。工具按 bot 记录最近主动对话后的普通文本和保护提醒下发次数，以及 24h 主动对话窗口。保护开启且普通文本预留成功时，工具会在用户请求正文末尾追加一行保护状态，显示本次发送计入后的剩余可发条数和距离限制触发的剩余时间；该状态来自同一次 Redis 预留，不需要外部调用者额外查询。接近次数限制或时间窗口限制时，工具会给当前上下文发送保护提醒并冻结普通文本发送；用户从微信 App 主动给机器人发一条消息后，工具解除冻结并重置该 bot 的计数和窗口。用户可通过 `/protection status` 查看当前 active bot 离触发限制还剩多少次数或时间，通过 `/protection disable` 关闭运行期保护。

每条普通文本发送成功前，工具会在发往微信的正文最底部追加一行 uuid v7 消息 ID，这个 ID 与审计开关无关、无运行期开关，HTTP API 请求 / 响应 JSON 不新增字段；这是一次有意的用户可见正文变化，自动化接收方依赖旧正文精确匹配时需要在消费端容忍或剥离末行 UUID。发送审计默认关闭、开启状态不写入 TOML，由服务进程写入 `~/.webot-msg/state/audit.json`。用户配置 Redis 后，在运行中控制台执行 `/audit enable` 开启；工具会在重启或升级后读取状态文件并尝试一次自动恢复，Redis 不可用时审计保持关闭、状态文件不被改写。审计开启后，每条普通文本成功发送会按消息 ID 向 Redis 写入发送时间 key 和完全体正文 key，TTL 分别由 `audit.time_ttl` 和 `audit.body_ttl`（默认各 24h）控制；审计写失败 fail open，不影响消息投递。经本地 API 发送且启用 telemetry 时，审计 Redis 写会在同一条 trace 下产生 span。用户可通过 `/audit status` 查看开关与 TTL、通过 `/audit disable` 关闭审计；保护提醒、typing、保护拒绝和 reminder-only 路径不带 ID、不审计。

在 Linux systemd 环境中，用户可以用 `scripts/linux-service.sh install` 编译 `bin/webot-msg`、安装 `/usr/local/bin/webot-msg`、创建 `~/.webot-msg/config/` 和 `~/.webot-msg/logs/`、首次写入默认 `webot-msg.toml`，并生成 `webot-msg.service`。升级时，脚本先按 `systemctl is-active` 记录服务是否运行：运行中则 stop、替换系统 PATH 中的二进制后再 start；未运行则只替换二进制。

## 边界

- 它只处理文本回复和 typing 状态，不负责富媒体消息编排。
- 它依赖最近消息提供可回复上下文；没有上下文时不能主动创建新会话。
- 它是本地运行工具，不提供多用户权限系统或公网部署安全边界。
- 它不替用户管理微信侧 bot 的生命周期，只保存本地登录和调用所需信息。
- Runtime config 不保存 bot token、API token、context token 或消息上下文；这些仍属于 auth store。
- 配置入口仅包含默认 `~/.webot-msg/config/webot-msg.toml`，不提供 CLI 参数、子命令或环境变量配置入口。
- 运行中服务的 control socket 只面向本机 Unix socket 客户端，不提供公网远程管理通道；无参 `webot-msg` 会在 socket 可连接时作为第一方 client 接入，但不提供单独的 attach 子命令。
- Tab 补全只覆盖直接前台 TTY 控制台和 TTY 下第一方 attach 控制台里已声明的控制台命令和固定子命令；不补全 bot 序号、botID、Redis 配置、文件路径、历史消息或普通文本。非 TTY 管道输入和 `nc -U` / `socat` 直连 control socket 不提供按键级补全。
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
- 消息 ID 与发送审计只作用于普通文本成功发送；保护提醒、typing、保护拒绝和 reminder-only 不带 ID、不审计。ID 不进 HTTP API 响应体，也无运行期开关。
- 发送审计与发送保护是相互独立的开关，复用同一份 `[redis]` 连接但互不影响；审计不为自己单独配置 Redis。审计 key 用 `{redis.key_prefix}:audit:time|body:{id}` 命名，不按 bot 隔离。
- 发送审计不提供查询 / 检索审计内容的命令；`/audit status` 只展示开关与 TTL，查看具体内容需直接访问 Redis。审计写失败 fail open，不像保护那样 fail closed。
- `audit:body` key 保存完整发送正文，属于敏感数据；保留期由 `audit.body_ttl` 控制，TTL 配置 `[audit]` 缺失时回退默认 24h，不会因此让旧配置启动失败。
- 审计 Redis 写只在请求已带入站 span 时产生 `audit.record` span（控制台触发不产生 root span），不引入 `redisotel` 自动 instrument。

## 变更记录

- 2026-06-10：新增 TOML Runtime config 能力，覆盖 API 端口、auth store 路径、iLink BaseURL、日志文件路径和大小上限；默认存储迁移到 `~/.webot-msg/`，并保留旧 `./config/auth.json` 到新默认路径的一次性复制兼容。
- 2026-06-10：新增 Linux systemd 部署脚本，支持 `install`、`upgrade` 和 `start` / `stop` / `restart` / `status` 服务控制；安装首次写入默认 Runtime config，升级只恢复原本 active 的服务。
- 2026-06-10：新增本地控制台 socket server，通过 Unix socket 进入 systemd 管理的运行中服务；`/exit` 和 `/quit` 只退出控制台连接，停止进程仍由 systemd 管理。
- 2026-06-10：服务和控制台默认读取 `~/.webot-msg/config/webot-msg.toml`，当时保留过旧配置路径兼容入口。
- 2026-06-10：Linux 部署脚本新增把二进制安装到 `/usr/local/bin/webot-msg`，让 `which webot-msg` 和系统命令在部署后直接可用。
- 2026-06-10：新增默认关闭的微信发送保护模式；开启后用 Redis 按 bot 记录下发计数和 24h 主动对话窗口，在临界时发送保护提醒、冻结普通文本发送，并在微信 App 主动消息到达后重置。
- 2026-06-10：发送保护开启状态从 Runtime config 移到运行中控制台命令；TOML 只保留 Redis 配置，新增 `/protection enable`、`/protection disable` 和 `/protection status`。
- 2026-06-11：直接前台 TTY 控制台新增 Tab 补全，覆盖 `/login`、`/exit`、`/protection` 等已声明命令和 `/protection enable|disable|status` 固定子命令。
- 2026-06-11：发送保护开关状态持久化到 `~/.webot-msg/state/protection.json`，服务重启或升级后尝试一次自动恢复，`/protection disable` 会写入关闭状态。
- 2026-06-11：第一方 socket attach 客户端曾支持本地行编辑和 Tab 补全；非 TTY 管道输入、第三方 socket 客户端和 socket 上行字节流契约保持行模式兼容。
- 2026-06-11：保护模式开启时，HTTP API 和控制台普通文本末尾追加保护状态行，显示本次发送计入后的剩余可发条数和距离限制触发的剩余时间；状态来自同一次 Redis 预留快照。
- 2026-06-12：唯一 CLI 入口收敛为无参 `webot-msg`；旧子命令和启动参数覆盖入口删除，带任何参数都会非零退出。socket server 保留。
- 2026-06-12：无参 `webot-msg` 改为优先接入已有 live control socket；socket 不可用时才启动新的前台 service。TTY attach 复用第一方行编辑和 Tab 补全，`nc -U` / `socat` 保留为第三方 line mode 入口。
- 2026-06-13：每条普通文本发送在正文最底部无条件拼接一行 uuid v7 消息 ID（用户可见正文变化，API 响应不变）。新增默认关闭、可运行期开关、可本地持久化（`~/.webot-msg/state/audit.json`）、启动一次性自动恢复的发送审计，开启后按消息 ID 向 Redis 写 `{prefix}:audit:time:{id}` 和 `{prefix}:audit:body:{id}`，TTL 由新增 `[audit]` 的 `time_ttl`/`body_ttl`（默认各 24h）控制；新增 `/audit enable|disable|status` 与 Tab 补全，审计 Redis 写在有入站 span 时接入 otel `audit.record` span。审计 fail open，与 fail closed 的保护相互独立，复用同一 `[redis]` 连接。
