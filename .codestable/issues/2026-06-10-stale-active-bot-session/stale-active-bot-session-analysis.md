---
doc_type: issue-analysis
issue: 2026-06-10-stale-active-bot-session
status: confirmed
root_cause_type: state-pollution
related: [stale-active-bot-session-report.md]
tags: [console, bot-session, ilink, messaging]
---

# 登录后仍使用旧 Bot 导致 Session Timeout 根因分析

## 1. 问题定位

| 关键位置 | 说明 |
|---|---|
| `internal/console/console.go:34` | 控制台会话启动时把 `controller.DefaultBotID()` 保存为 session-local `activeBotID`。 |
| `internal/console/console.go:65` | `/login` 成功后只在 `activeBotID == ""` 时才切换到新 bot。已有 active bot 时，新登录 bot 不会成为当前发送身份。 |
| `internal/app/app.go:100` | `App.Login` 会保存新 bot 并启动监听，然后返回新 `botID`；调用方拿到了新 bot，但控制台没有使用它覆盖当前 active bot。 |
| `internal/app/app.go:148` | 普通文本发送按 `activeBotID` 取本地 bot 配置，因此日志中登录成功的是 `3b4a...`，实际发送却仍走提示符里的 `e927...`。 |
| `internal/ilink/client.go:178` | `SendMessage` 使用旧 bot 的 `BotToken`、`IlinkUserID` 和 `ContextToken` 组装远端请求；旧 session 过期时 iLink 返回 `errcode=-14 session timeout`。 |
| `internal/app/app.go:207` | 监听循环调用 `GetUpdates` 失败后只 sleep + continue，没有记录错误；过期 bot 的监听失败不会暴露给用户。 |
| `internal/app/app.go:230` | 收到新消息时检查了 `msg.FromUserID`，但只持久化 `ContextToken`，没有把 `IlinkUserID` 更新为最近消息的 `FromUserID`，会让发送目标和上下文存在配对不一致风险。 |

## 2. 失败路径还原

**正常路径**：用户登录 bot -> 控制台 active bot 指向可用 bot -> 监听收到消息并保存最近会话上下文 -> 用户输入文本 -> `SendText` 使用同一个 bot 的有效 token 和最近上下文调用 `sendmessage` -> 发送成功。

**失败路径**：控制台已有旧 active bot `e927...` -> 用户执行 `/login` 登录新 bot `3b4a...` -> `console.RunWithIO` 因为 `activeBotID != ""` 不切换 active bot -> 用户直接输入 `qq` -> `App.SendText` 仍读取 `e927...` 的配置 -> 旧 bot session 已过期 -> iLink 返回 `errcode=-14 session timeout`。

**分叉点**：`internal/console/console.go:69` — `/login` 成功后保留旧 active bot，而用户界面刚输出新 bot 登录成功，导致“登录成功”和“当前发送身份”不一致。

## 3. 根因

**根因类型**：state-pollution

**根因描述**：控制台的 active bot 是会话本地状态，不会随 `App.Login` 自动更新。当前实现只在没有 active bot 时把新登录 bot 设为 active；当旧 bot 已经存在时，登录新 bot 不影响当前 active bot。用户看到 `Login confirmed! BotID: 3b4a...` 后自然认为后续发送会使用新 bot，但提示符实际仍是 `[e927...]`，发送链路继续使用过期的旧 bot token 和上下文，最终得到 iLink `session timeout`。

**是否有多个根因**：是。

主根因是 `/login` 后 active bot 状态没有切换或提示，造成发送使用旧 bot。次要根因是监听错误被静默吞掉，过期 bot 的 getupdates 失败表现成“收不到消息”而不是明确的 session 过期。另一个潜在链路问题是 `persistUpdateState` 没有同步最近消息的 `FromUserID` 到 `IlinkUserID`，可能让发送目标和 `ContextToken` 不匹配。

## 4. 影响面

- **影响范围**：所有已有 active bot 时再次 `/login` 的控制台会话；旧 bot session 过期时最明显。
- **潜在受害模块**：`internal/console` 的交互发送、`internal/control` 通过 Unix socket 进入的控制台、`internal/app` 的监听状态提示、`internal/api` 对最近上下文的发送链路。
- **数据完整性风险**：不会破坏本地配置结构，但可能保存和继续使用过期 token / 过期 context；如果 `IlinkUserID` 与 `ContextToken` 不配对，可能导致发送失败或回错会话。
- **严重程度复核**：维持 P1。核心收发链路受损，但用户可以通过 `/bots` 手动切换新 bot 或删除旧 bot 绕过。

## 5. 修复方案

### 方案 A：`/login` 成功后自动切换到新 bot

- **做什么**：修改 `internal/console/console.go`，`/login` 成功后总是把 `activeBotID = botID`，并输出当前 active bot 已切换；补充控制台测试覆盖“已有 active bot 时登录后发送使用新 bot”。
- **优点**：最符合用户直觉；改动最小；直接解决日志中登录新 bot 后仍发旧 bot 的问题。
- **缺点 / 风险**：如果用户只是想添加 bot 不想切换，行为会变化；但 `/login` 是强交互动作，自动切换更合理。
- **影响面**：只影响控制台 active bot 状态，不改变 store schema 和远端请求格式。

### 方案 B：保留旧 active bot，但登录后强提示并要求手动选择

- **做什么**：`/login` 成功后如果已有 active bot，不切换，但输出“new bot added; current active remains ...; use /bot ... to switch”；可在 `/bots` 中突出提示。
- **优点**：行为兼容，不改变现有 active bot 语义。
- **缺点 / 风险**：仍然需要用户手动操作，容易继续误用旧 bot；不能直接修复“登录后马上发送失败”的体验。
- **影响面**：只改控制台提示和测试。

### 方案 C：同时修复 active bot、监听错误可见性和最近会话目标更新

- **做什么**：采用方案 A；在 `monitorWeixin` 对 getupdates 错误做节流日志，至少对 `session timeout` 可见；在 `persistUpdateState` 保存 `ContextToken` 时同步 `IlinkUserID = msg.FromUserID`，补测试覆盖上下文配对。
- **优点**：覆盖“发错旧 bot”和“收不到消息没有提示”两类问题，也降低后续目标 / context 不匹配风险。
- **缺点 / 风险**：改动跨 `console` 和 `app`，需要更仔细测试日志节流和上下文语义。
- **影响面**：控制台状态、监听日志、本地 auth store 中 `IlinkUserID` 更新策略。

### 推荐方案

**推荐方案 C**，但分两步落地：先用方案 A 修掉当前直接失败路径，再加监听错误节流日志和 `IlinkUserID` / `ContextToken` 配对更新。理由是当前用户看到的 `3b4a...` 登录成功后仍在 `[e927...]` 发送，是明确的 active bot 状态问题；同时“收不到消息”需要监听失败可见性，否则下次 session 过期仍难定位。
