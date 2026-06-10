---
doc_type: issue-report
issue: 2026-06-10-control-console-message-broadcast
status: confirmed
severity: P1
summary: systemd control console 能发送消息但看不到微信发来的监听消息
tags: [control-console, messaging, systemd, output]
---

# Control Console 看不到监听消息 Issue Report

## 1. 问题现象

用户通过控制台 `/login` 成功登录新 bot，提示符已切到新 bot，向微信发送文本也返回 `Send success!`。但从微信侧发消息后，当前控制台看不到任何收到消息的输出。

## 2. 复现步骤

1. 启动运行中的 `webot-msg` 服务。
2. 通过 `webot-msg console` 连接服务控制台。
3. 执行 `/login` 并扫码确认。
4. 在控制台输入文本发送，看到 `Send success!`。
5. 从微信侧给 bot 发消息。
6. 观察到：当前控制台没有显示收到的消息。

复现频率：稳定，使用 control console / systemd 运行形态时可复现。

## 3. 期望 vs 实际

**期望行为**：运行中服务收到微信消息时，当前连接的 `webot-msg console` 能看到消息输出。

**实际行为**：监听协程收到消息后只写服务进程 stdout，不会写到 control socket 连接；控制台可发送但看不到监听输出。

## 4. 环境信息

- 涉及模块 / 功能：`internal/app` 消息监听输出、`internal/control` Unix socket 控制台。
- 相关文件 / 函数：`internal/app/app.go:260`、`internal/control/server.go:50`。
- 运行环境：Linux systemd / `webot-msg console`。
- 其他上下文：auth store 脱敏摘要显示新 bot 已有 `get_updates_buf` 与 `context_token`，说明监听链路推进过状态。

## 5. 严重程度

**P1** — 核心交互体验受损；用户无法在控制台观察收到消息，但发送链路仍可用。

## 备注

这是前一个 `stale-active-bot-session` issue 修复后暴露出的独立输出路由问题。
