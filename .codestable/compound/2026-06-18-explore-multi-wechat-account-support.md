---
doc_type: explore
type: question
date: 2026-06-18
slug: multi-wechat-account-support
topic: 当前项目是否支持不同微信账号接入
scope: internal/config, internal/app, internal/console, internal/api, internal/protection, docs/user
keywords: [wechat, bot, account, login, auth-store, api]
status: active
confidence: high
---

## 问题与范围

问题：当前项目支持不同微信账号的接入吗？

范围：只判断当前代码与项目文档已经实现的能力，不推断 iLink 上游对账号数量、设备限制或风控策略的外部规则。

## 速答

支持，但支持粒度是“多个已扫码登录的 bot / 微信账号配置共存”，不是完整多租户账号体系。

当前实例可以通过 `/login` 多次扫码添加 bot，每个 bot 以 `bot_id` 为 key 保存自己的 `bot_token`、`api_token`、更新游标和最近消息上下文。启动后会为 auth store 中每个 bot 启动监听；控制台通过 active bot 选择发送账号，HTTP API 通过 `/bots/{botID}/...` 和该 bot 的 `api_token` 选择发送账号。

限制也明确：控制台发送必须先选 active bot；HTTP API 必须知道目标 `botID` 和对应 `api_token`；发送目标仍是该 bot 当前保存的最近会话上下文，不是任意指定联系人。相同 `bot_id` 再登录会覆盖原配置。

```mermaid
flowchart LR
  QR[/login QR scan] --> Login[App.Login]
  Login --> Store[(auth.json bots map)]
  Store --> Monitor[per-bot monitor]
  Store --> Console[console active bot]
  Store --> API[/bots/{botID}/messages]
  Console --> Send[SendText selected bot]
  API --> Token[check bot api_token]
  Token --> Send
```

## 关键证据

1. auth store 是多 bot map，而不是单账号结构：`AppConfig.Bots map[string]*UserConfig`，每个 `UserConfig` 保存 bot token、bot id、游标、上下文和 API token。证据：`internal/config/store.go:21`、`internal/config/store.go:30`。
2. 新登录会按 `user.BotID` 写入 map 并保存；这允许多个不同 bot id 共存，也意味着相同 bot id 会覆盖旧值。证据：`internal/config/store.go:142`。
3. 启动时如果已有 bot，会打印加载数量，并遍历所有 `BotIDs()` 启动监听。证据：`internal/app/app.go:133`、`internal/app/app.go:162`。
4. `/login` 调用 QR 登录后 `AddBot`，随后为该 bot 启动监听；控制台登录成功后把新 bot 设为 active bot。证据：`internal/app/app.go:193`、`internal/app/app.go:201`、`internal/console/console.go:81`。
5. 控制台把 `activeBotID` 保存在当前控制台会话里，支持 `/bots` 和 `/bot <num>` 选择，普通文本发送时传入 active bot。证据：`internal/console/console.go:49`、`internal/console/console.go:92`、`internal/console/console.go:112`、`internal/console/console.go:150`。
6. HTTP API 路径从 `/bots/{botID}/...` 解析 bot id，再按该 bot 的 `APIToken` 鉴权；发送和 typing 都使用查到的 `UserConfig`。证据：`internal/api/server.go:88`、`internal/api/server.go:112`、`internal/api/server.go:117`、`internal/api/server.go:122`。
7. 保护状态按 bot 隔离，Redis key 使用 `{botID}` 作为 hash tag；测试覆盖了 `bot-A` 和 `bot-B` 的计数隔离。证据：`internal/protection/redis_guard.go:181`、`internal/protection/redis_guard_test.go:453`。

## 细节展开

`Store` 是这个能力的基础。`auth.json` 的根结构是 `{"bots": {...}}`，业务入口都围绕 `botID` 查 `UserConfig`。`ListBots` 和 `BotIDs` 会从 map 中取有效 bot id 并排序，所以控制台列表和启动监听都能天然处理多个 bot。

运行时也按 bot 分离。`App.Run` 启动时对每个 bot 执行 `startMonitor`；`startMonitor` 用 `runningMonitors` 防止同一个 bot 重复监听。监听拿到消息后只更新对应 bot 的 `GetUpdatesBuf`、`IlinkUserID` 和 `ContextToken`。

控制台是“当前会话 active bot”模型。只有一个 bot 时 `DefaultBotID()` 自动选中；多个 bot 时不自动选，需要 `/bots` 或 `/bot <num>` 选择。这个 active 状态没有写回全局配置，所以不同控制台连接可以各自选择。

HTTP API 是显式 bot 模型。调用方必须请求 `/bots/{botID}/messages` 或 `/bots/{botID}/typing`，并提供该 bot 自己的 `api_token`。token 不能跨 bot 使用。

## 未决问题

- 代码没有保存微信账号昵称、UIN 或展示名；用户只能通过 `bot_id` 区分不同账号。
- 代码层面支持多个 bot，但没有看到对上游 iLink / 微信侧账号数量、同时在线限制、风控限制的声明。
- API 发送只回复该 bot 当前保存的最近消息上下文；不支持在请求中指定任意联系人或会话。

## 后续建议

如果要把“多微信账号接入”做成对外明确能力，建议补用户文档：如何多次 `/login`、如何查看 `bot_id`、如何为 API 调用选择 bot，以及多个 bot 的上下文限制。

## 相关文档

- `.codestable/architecture/ARCHITECTURE.md`
- `docs/user/runtime-config.md`
- `docs/user/linux-systemd-deploy.md`
