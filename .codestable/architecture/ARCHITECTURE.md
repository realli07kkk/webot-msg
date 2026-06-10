---
doc_type: architecture
slug: architecture-overview
scope: webot-msg 当前 CLI/API 服务整体结构
summary: CLI 启动一个本地应用，由 app 编排配置、iLink 客户端、HTTP API、控制台、每个 bot 的消息监听和 Linux systemd 部署脚本。
status: current
last_reviewed: 2026-06-10
tags: [go, cli, api, bot, config, logging, deploy, systemd]
depends_on: []
implements:
  - bot-message-bridge
---

# webot-msg 架构总入口

## 0. 术语

- Bot：本文指一个已扫码登录的微信 bot 配置项，对应 `config.UserConfig`，包含 bot token、bot id、更新游标、最近消息上下文和本地 API token。代码锚点：`internal/config/store.go:21`。
- Active bot：控制台当前选中的发送身份，保存在 `app.App.activeBot`，由互斥锁保护。代码锚点：`internal/app/app.go:24`。
- 消息上下文：发送回复需要的 `IlinkUserID` 与 `ContextToken`，由监听更新时写回本地配置。代码锚点：`internal/app/app.go:226`。
- Runtime config：启动时读取的 TOML 配置，控制 API 端口、auth store 路径、iLink BaseURL 和日志文件策略，不保存 bot 凭据或消息上下文。代码锚点：`internal/runtimeconfig/config.go:26`。
- Log file policy：标准日志的文件输出路径和大小上限，默认写入 `~/.webot-msg/logs/webot-msg.log`，达到上限后只保留一个 `.1` 备份。代码锚点：`internal/logfile/writer.go:9`。
- Linux deploy script：仓库内的 Linux/systemd 编译部署入口，负责编译 `bin/webot-msg`、首次写入默认 Runtime config、安装 `webot-msg.service`、升级时按原运行状态 stop/start。代码锚点：`scripts/linux-service.sh:1`。
- Service unit：Linux deploy script 生成的 systemd unit，固定名为 `webot-msg.service`，用 `ExecStart={repo}/bin/webot-msg -c {home}/.webot-msg/config/webot-msg.toml` 启动服务。代码锚点：`scripts/linux-service.sh:206`。

## 1. 定位与受众

本项目是一个 Go 写的本地 CLI/API 服务，用来扫码登录微信 bot、监听消息上下文，并从控制台或 HTTP API 发送文本回复。读者主要是做 feature-design、issue-analyze 或新接手项目的人；读完应能定位入口、状态归属、外部调用边界和凭证风险。

## 2. 结构与交互

`cmd/webot-msg/main.go` 是唯一可执行入口，解析 `-c` 和 `-port`，加载并校验 Runtime config，准备本地存储目录和文件日志，然后用解析后的 auth store path 与 iLink BaseURL 创建应用。`-port` 是兼容覆盖入口：同时存在 TOML `api.port` 和显式 `-port` 时以命令行值为准。代码锚点：`cmd/webot-msg/main.go:13`、`cmd/webot-msg/main.go:42`。

`internal/runtimeconfig` 是启动配置计算层。它先给出内置默认值，再按可选 TOML 覆盖，最后做 `~` 展开、端口范围、BaseURL scheme、日志大小和未知 key 校验。默认存储根目录是 `~/.webot-msg/`，默认 auth store 与日志路径分别落在 `config/` 和 `logs/` 子目录。代码锚点：`internal/runtimeconfig/config.go:16`、`internal/runtimeconfig/config.go:51`、`internal/runtimeconfig/config.go:69`、`internal/runtimeconfig/config.go:90`。

`scripts/linux-service.sh` 是 Linux/systemd 部署编排入口。`install` 会检查 Linux/systemd、Go、部署用户 home 和 sudo 权限，编译当前源码到 `bin/webot-msg`，创建部署用户的 `~/.webot-msg/config/` 与 `~/.webot-msg/logs/`，首次写入默认 `webot-msg.toml`，再生成 `/etc/systemd/system/webot-msg.service` 并执行 `systemctl daemon-reload`。`upgrade` 会先用 `systemctl is-active --quiet webot-msg` 记录服务是否 active；active 时先 stop，替换二进制后再 start；非 active 时只替换二进制，不主动启动。`start`、`stop`、`restart`、`status` 子命令透传到 `systemctl`。代码锚点：`scripts/linux-service.sh:261`、`scripts/linux-service.sh:272`、`scripts/linux-service.sh:298`。

`internal/logfile` 是标准日志文件输出的轻量大小控制层。传入空日志路径时禁用文件日志；传入路径时以追加方式打开文件，并在下一次写入会超过上限时把当前文件轮转为 `.1`。代码锚点：`internal/logfile/writer.go:17`、`internal/logfile/writer.go:47`、`internal/logfile/writer.go:74`。

`internal/app` 是编排层，持有配置仓库、iLink 客户端、当前 active bot 和正在运行的监听协程集合。它负责启动时加载配置、必要时扫码登录、补齐 API token、启动监听、启动 HTTP API，再进入控制台循环。代码锚点：`internal/app/app.go:20`、`internal/app/app.go:39`。

`internal/api` 暴露 `/bots/{botID}/messages` 和 `/bots/{botID}/typing` 两类动作。请求先从 `Authorization: Bearer` 或参数里取 token，再按 bot id 查本地配置并校验 `APIToken`，通过后调用 iLink 客户端。代码锚点：`internal/api/server.go:27`、`internal/api/server.go:36`。

`internal/console` 只依赖 `Controller` 接口，负责 `/login`、`/bots`、`/bot <num>`、`/del <num>`、`/exit`、`/quit` 和普通文本发送。这个接口让控制台不直接依赖 app 的具体结构；控制台会返回退出原因，让 `app` 区分用户主动退出和 stdin 关闭。代码锚点：`internal/console/console.go:18`、`internal/console/console.go:27`。

`internal/ilink` 是外部 HTTP API 适配层，封装 QR 登录、拉取更新、发送消息、发送 typing 状态和 bot 配置读取。所有远端请求都通过 `Client.BaseURL` 组装 endpoint。代码锚点：`internal/ilink/client.go:21`、`internal/ilink/client.go:57`、`internal/ilink/client.go:131`、`internal/ilink/client.go:174`。

## 3. 数据与状态

持久化入口是 `config.Store`。auth store 的 JSON schema 不变，仍保存 bot token、API token、更新游标和消息上下文；默认运行时路径从旧的 `./config/auth.json` 迁到 `~/.webot-msg/config/auth.json`。Runtime config 是独立 TOML，不进入 auth store。仓库用互斥锁保护内存中的 `AppConfig`，读写 bot 列表、token、更新游标和消息上下文时都通过 Store 方法。代码锚点：`internal/config/store.go:14`、`internal/config/store.go:40`、`internal/runtimeconfig/config.go:18`。

`UserConfig` 是核心持久化结构。`BotToken` 用于调用 iLink，`APIToken` 用于保护本地 HTTP API，`GetUpdatesBuf` 是拉取更新的游标，`IlinkUserID` 与 `ContextToken` 是回复最近会话的上下文。代码锚点：`internal/config/store.go:21`。

监听状态按 bot 分协程运行，`runningMonitors` 防止同一个 bot 重复启动监听。每次拉取更新后，app 将更新游标和新的 context token 写回 Store，再打印消息内容。代码锚点：`internal/app/app.go:182`、`internal/app/app.go:194`、`internal/app/app.go:226`。

兼容迁移只在使用默认 auth store path 时发生：如果旧 `./config/auth.json` 存在且新 `~/.webot-msg/config/auth.json` 不存在，启动前原样复制一次；如果新文件已存在，旧文件不会覆盖新文件。复制目标文件按 owner-only 权限创建。代码锚点：`internal/runtimeconfig/config.go:151`、`internal/runtimeconfig/config.go:208`。

Linux deploy script 首次安装时会写入默认 Runtime config 文件 `~/.webot-msg/config/webot-msg.toml`，内容与 Runtime config 默认契约一致；已有 TOML 时不会覆盖，`~/.webot-msg/config/auth.json` 仍由 `config.Store` 管理，不由部署脚本迁移、覆盖或删除。代码锚点：`scripts/linux-service.sh:167`。

## 4. 关键决策

- Runtime config 与 auth store 分离：启动参数使用 TOML，运行态凭据、游标和上下文继续放在 auth store JSON 中，避免把可提交的启动配置和本地凭据混在一起。
- 默认本地存储统一收敛到 `~/.webot-msg/`：默认 auth store 使用 `~/.webot-msg/config/auth.json`，默认标准日志使用 `~/.webot-msg/logs/webot-msg.log`；显式 TOML 路径可以指向其他位置。
- 配置入口保持克制：本项目只有 `-c` TOML 和兼容 `-port` 两类启动配置入口，不新增环境变量配置入口。
- auth store 权限按凭据处理：新建 auth 目录使用 owner-only，auth 文件保存和 legacy copy 后都保持 owner-only；日志文件不使用 auth store 权限策略。
- Linux 部署入口保持仓库脚本形态：本项目提供 Bash 脚本管理单个 `webot-msg.service`，不引入 `.deb`、RPM、Docker、Ansible 或多实例管理。
- 升级保持原运行意图：`upgrade` 只在服务升级前处于 active 时 stop 后再 start；服务原本非 active 时只替换二进制，不主动启动。

## 5. 代码锚点

- `cmd/webot-msg/main.go:main` — CLI 入口，负责参数解析和 app 启动。
- `internal/runtimeconfig/config.go:Config` — TOML Runtime config 的结构、默认值、校验和存储准备。
- `internal/logfile/writer.go:SizeWriter` — 标准日志文件输出和简单大小轮转。
- `internal/app/app.go:App.Run` — 启动编排主流程。
- `internal/app/app.go:monitorWeixin` — 每个 bot 的长轮询监听循环。
- `internal/api/server.go:handleBotAction` — HTTP API 鉴权和动作分发。
- `internal/config/store.go:Store` — 本地配置持久化和并发保护。
- `internal/console/console.go:Run` — 交互式控制台命令循环。
- `internal/ilink/client.go:Client` — iLink HTTP 调用封装。
- `scripts/linux-service.sh:cmd_install` — Linux/systemd 安装编排，构建二进制、写默认配置和安装 service。
- `scripts/linux-service.sh:cmd_upgrade` — Linux/systemd 升级编排，按服务原 active 状态 stop/start。

## 6. 已知约束 / 边界情况

- auth store 包含 bot token、API token 和消息上下文，不能提交到 Git；默认运行时路径是 `~/.webot-msg/config/auth.json`，旧 `./config/auth.json` 只作为一次性兼容复制源。来源：`.gitignore`、`internal/config/store.go:14`、`internal/runtimeconfig/config.go:18`。
- 未传 `-c` 时不要求 TOML 文件存在，直接使用内置默认值；显式 `-port` 会覆盖 TOML `api.port`。代码锚点：`cmd/webot-msg/main.go:47`、`cmd/webot-msg/main.go:58`。
- Runtime config 使用严格 TOML 模式，未知 section / key 会启动失败；`ilink.base_url` 只接受 `http` 或 `https` scheme。代码锚点：`internal/runtimeconfig/config.go:80`、`internal/runtimeconfig/config.go:255`。
- 默认存储路径统一在 `~/.webot-msg/` 下；默认 auth store 目录和 auth 文件按 owner-only 权限处理。代码锚点：`internal/runtimeconfig/config.go:18`、`internal/runtimeconfig/config.go:135`、`internal/config/store.go:17`。
- 文件日志只接管标准库 `log` 输出，不接管控制台提示、二维码或收到的消息内容；启动摘要不能记录 bot token、API token、context token 或完整消息正文。代码锚点：`cmd/webot-msg/main.go:31`、`cmd/webot-msg/main.go:34`。
- 发送文本依赖最近消息上下文；如果 active bot 没有 `IlinkUserID` 或 `ContextToken`，控制台和 API 都会拒绝发送。代码锚点：`internal/app/app.go:152`、`internal/api/server.go:86`。
- HTTP API token 为空或不匹配都会返回 unauthorized，不能绕过本地 `APIToken` 校验。代码锚点：`internal/api/server.go:65`。
- 服务关闭支持两条路径：控制台 `/exit` 或 `/quit` 会保存配置并从 `App.Run` 返回；收到 `os.Interrupt` 和 `SIGTERM` 信号时也会保存配置并退出。stdin 关闭不是主动退出，服务会继续后台运行。代码锚点：`internal/app/app.go:78`、`internal/app/app.go:168`。
- Linux deploy script 只面向 Linux/systemd 单实例部署；它会拒绝非 Linux 或未运行 systemd 的环境。代码锚点：`scripts/linux-service.sh:47`。
- `webot-msg.service` 使用部署用户运行，`ExecStart` 中的二进制路径和配置路径必须是绝对路径；脚本拒绝写入包含空白字符的 systemd 路径，避免 unit 解析歧义。代码锚点：`scripts/linux-service.sh:116`、`scripts/linux-service.sh:206`。
- 部署脚本不会覆盖已有 `~/.webot-msg/config/webot-msg.toml` 或删除 `~/.webot-msg/config/auth.json`；真实 Linux systemd 主机上的服务操作仍需要部署者具备 sudo 权限。代码锚点：`scripts/linux-service.sh:65`、`scripts/linux-service.sh:167`。

## 7. 相关文档

- `.codestable/requirements/bot-message-bridge.md` — 当前用户可感能力描述。
- `docs/user/linux-systemd-deploy.md` — Linux systemd 安装、升级和服务控制说明。
- `.codestable/requirements/VISION.md` — requirement 索引。
- `.codestable/attention.md` — CodeStable 技能启动必读的项目注意事项入口。
