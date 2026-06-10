---
doc_type: architecture
slug: architecture-overview
scope: webot-msg 当前 CLI/API 服务整体结构
summary: CLI 启动一个本地应用，由 app 编排配置、iLink 客户端、HTTP API、控制台和每个 bot 的消息监听。
status: current
last_reviewed: 2026-06-10
tags: [go, cli, api, bot]
depends_on: []
implements:
  - bot-message-bridge
---

# webot-msg 架构总入口

## 0. 术语

- Bot：本文指一个已扫码登录的微信 bot 配置项，对应 `config.UserConfig`，包含 bot token、bot id、更新游标、最近消息上下文和本地 API token。代码锚点：`internal/config/store.go:16`。
- Active bot：控制台当前选中的发送身份，保存在 `app.App.activeBot`，由互斥锁保护。代码锚点：`internal/app/app.go:24`。
- 消息上下文：发送回复需要的 `IlinkUserID` 与 `ContextToken`，由监听更新时写回本地配置。代码锚点：`internal/app/app.go:226`。

## 1. 定位与受众

本项目是一个 Go 写的本地 CLI/API 服务，用来扫码登录微信 bot、监听消息上下文，并从控制台或 HTTP API 发送文本回复。读者主要是做 feature-design、issue-analyze 或新接手项目的人；读完应能定位入口、状态归属、外部调用边界和凭证风险。

## 2. 结构与交互

`cmd/webot-msg/main.go` 是唯一可执行入口，解析 `-port` 后用默认配置路径和 iLink base URL 创建应用。代码锚点：`cmd/webot-msg/main.go:12`。

`internal/app` 是编排层，持有配置仓库、iLink 客户端、当前 active bot 和正在运行的监听协程集合。它负责启动时加载配置、必要时扫码登录、补齐 API token、启动监听、启动 HTTP API，再进入控制台循环。代码锚点：`internal/app/app.go:20`、`internal/app/app.go:39`。

`internal/api` 暴露 `/bots/{botID}/messages` 和 `/bots/{botID}/typing` 两类动作。请求先从 `Authorization: Bearer` 或参数里取 token，再按 bot id 查本地配置并校验 `APIToken`，通过后调用 iLink 客户端。代码锚点：`internal/api/server.go:27`、`internal/api/server.go:36`。

`internal/console` 只依赖 `Controller` 接口，负责 `/login`、`/bots`、`/bot <num>`、`/del <num>` 和普通文本发送。这个接口让控制台不直接依赖 app 的具体结构。代码锚点：`internal/console/console.go:11`、`internal/console/console.go:20`。

`internal/ilink` 是外部 HTTP API 适配层，封装 QR 登录、拉取更新、发送消息、发送 typing 状态和 bot 配置读取。所有远端请求都通过 `Client.BaseURL` 组装 endpoint。代码锚点：`internal/ilink/client.go:21`、`internal/ilink/client.go:57`、`internal/ilink/client.go:131`、`internal/ilink/client.go:174`。

## 3. 数据与状态

持久化入口是 `config.Store`，默认文件为 `./config/auth.json`。仓库用互斥锁保护内存中的 `AppConfig`，读写 bot 列表、token、更新游标和消息上下文时都通过 Store 方法。代码锚点：`internal/config/store.go:14`、`internal/config/store.go:35`。

`UserConfig` 是核心持久化结构。`BotToken` 用于调用 iLink，`APIToken` 用于保护本地 HTTP API，`GetUpdatesBuf` 是拉取更新的游标，`IlinkUserID` 与 `ContextToken` 是回复最近会话的上下文。代码锚点：`internal/config/store.go:16`。

监听状态按 bot 分协程运行，`runningMonitors` 防止同一个 bot 重复启动监听。每次拉取更新后，app 将更新游标和新的 context token 写回 Store，再打印消息内容。代码锚点：`internal/app/app.go:182`、`internal/app/app.go:194`、`internal/app/app.go:226`。

## 4. 关键决策

TODO: 当前仓库还没有 `.codestable/compound/` decision 文档。后续如果长期保留“本地 JSON 持久化”“控制台与 HTTP API 共用同一 app 编排层”等选择，应使用 `cs-decide` 归档后再在这里引用。

## 5. 代码锚点

- `cmd/webot-msg/main.go:main` — CLI 入口，负责参数解析和 app 启动。
- `internal/app/app.go:App.Run` — 启动编排主流程。
- `internal/app/app.go:monitorWeixin` — 每个 bot 的长轮询监听循环。
- `internal/api/server.go:handleBotAction` — HTTP API 鉴权和动作分发。
- `internal/config/store.go:Store` — 本地配置持久化和并发保护。
- `internal/console/console.go:Run` — 交互式控制台命令循环。
- `internal/ilink/client.go:Client` — iLink HTTP 调用封装。

## 6. 已知约束 / 边界情况

- `config/auth.json` 包含 bot token、API token 和消息上下文，不能提交到 Git。来源：`.gitignore` 和 `internal/config/store.go:14`。
- 发送文本依赖最近消息上下文；如果 active bot 没有 `IlinkUserID` 或 `ContextToken`，控制台和 API 都会拒绝发送。代码锚点：`internal/app/app.go:152`、`internal/api/server.go:86`。
- HTTP API token 为空或不匹配都会返回 unauthorized，不能绕过本地 `APIToken` 校验。代码锚点：`internal/api/server.go:65`。
- 服务关闭时只处理 `os.Interrupt` 和 `SIGTERM`，收到信号后保存配置并退出。代码锚点：`internal/app/app.go:168`。

## 7. 相关文档

- `.codestable/requirements/bot-message-bridge.md` — 当前用户可感能力描述。
- `.codestable/requirements/VISION.md` — requirement 索引。
- `.codestable/attention.md` — CodeStable 技能启动必读的项目注意事项入口。
