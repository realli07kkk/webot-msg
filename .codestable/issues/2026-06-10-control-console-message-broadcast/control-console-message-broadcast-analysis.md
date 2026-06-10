---
doc_type: issue-analysis
issue: 2026-06-10-control-console-message-broadcast
status: confirmed
root_cause_type: logic
related: [control-console-message-broadcast-report.md]
tags: [control-console, messaging, systemd, output]
---

# Control Console 看不到监听消息根因分析

## 1. 问题定位

| 关键位置 | 说明 |
|---|---|
| `internal/control/server.go:50` | 每个 Unix socket 连接只被交给 `console.RunWithIO` 处理命令交互，监听协程没有这个连接的输出引用。 |
| `internal/app/app.go:228` | 监听协程收到 `GetUpdates` 返回后调用 `printMessages` 打印消息。 |
| `internal/app/app.go:260` | `printMessages` 使用全局 `fmt.Printf` 写服务进程 stdout；systemd/control console 模式下，这不是当前 socket 控制台的输出。 |

## 2. 失败路径还原

**正常路径**：服务收到微信消息 -> 监听协程渲染消息文本 -> 当前连接的控制台收到消息输出 -> 用户看到消息并回复。

**失败路径**：服务收到微信消息 -> 监听协程渲染消息文本 -> `fmt.Printf` 写到服务 stdout / journal -> control socket 连接没有收到这段输出 -> 用户看到控制台无消息。

**分叉点**：`internal/app/app.go:260` — 消息输出硬编码到进程 stdout，没有输出到 control console 连接。

## 3. 根因

**根因类型**：logic

**根因描述**：控制台命令输出和监听消息输出走了两条不同通道。`webot-msg console` 的 socket 连接只参与 `console.RunWithIO` 的命令读写；监听协程在 `App` 内部运行，不知道哪些 socket 控制台正在连接，因此收到消息时只能写服务 stdout。systemd 模式下用户看的不是服务 stdout，所以表现为“能发但收不到”。

**是否有多个根因**：否。消息结构解析是否完整仍有后续观察空间，但本次日志和 auth store 状态足以定位当前主要问题为输出路由。

## 4. 影响面

- **影响范围**：所有通过 `webot-msg console` 连接运行中 service 的用户。
- **潜在受害模块**：`internal/control` socket 控制台、`internal/app` 监听输出。
- **数据完整性风险**：无；不影响 auth store 和远端发送，只影响用户可见输出。
- **严重程度复核**：维持 P1，因为核心观测体验不可用，但消息上下文和发送链路仍可工作。

## 5. 修复方案

### 方案 A：控制台连接注册为消息输出订阅者

- **做什么**：`control.Server` 接受 socket 连接时，把连接写端注册到 `App`；`App.printMessages` 写服务 stdout 的同时广播到所有已连接控制台。
- **优点**：直接修复 control console 看不到消息的问题；保留原 stdout 行为；不把消息内容写进日志文件。
- **缺点 / 风险**：多个控制台连接会同时看到所有 bot 的消息；本地 control socket 本来就是 owner-only，本风险可接受。
- **影响面**：`internal/app`、`internal/control`。

### 方案 B：把消息写入标准日志 / journal

- **做什么**：把 `printMessages` 改成 `log.Printf`，让 systemd 用户通过 journal 查看消息。
- **优点**：实现最小。
- **缺点 / 风险**：完整消息正文进入日志，不符合现有安全边界；也不能满足“当前控制台直接看到”的体验。
- **影响面**：`internal/app`、日志策略文档。

### 推荐方案

**推荐方案 A**。它修复当前用户可见问题，同时避免把完整消息长期写入日志。
