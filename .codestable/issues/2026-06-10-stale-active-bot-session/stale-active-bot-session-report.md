---
doc_type: issue-report
issue: 2026-06-10-stale-active-bot-session
status: confirmed
severity: P1
summary: 登录新 bot 后控制台仍使用旧 bot，发送失败并返回 session timeout，同时监听失败不明显
tags: [console, bot-session, ilink, messaging]
---

# 登录后仍使用旧 Bot 导致 Session Timeout Issue Report

## 1. 问题现象

控制台扫码登录显示新 bot 登录成功：

```text
Login confirmed! BotID: 3b4a2e607b0b@im.bot
```

但登录后提示符仍显示旧 bot：

```text
[e927e2b8dcf0@im.bot] > qq
Send failed: API Error: ret=0, errcode=-14, msg=session timeout
```

用户体感是新登录后仍然收不到消息 / 不能正常发送，错误来自 iLink 的 `session timeout`。

## 2. 复现步骤

1. 已存在一个旧 bot，例如 `e927e2b8dcf0@im.bot`，控制台当前 active bot 指向它。
2. 在控制台执行 `/login` 并扫码确认新 bot，例如 `3b4a2e607b0b@im.bot`。
3. 登录成功后不手动执行 `/bots` 或 `/bot <num>`，直接输入普通文本发送。
4. 观察到：提示符仍是旧 bot，发送返回 `errcode=-14, msg=session timeout`。

复现频率：稳定，前提是控制台登录前已经有 active bot，且旧 bot 的远端 session 已失效。

## 3. 期望 vs 实际

**期望行为**：扫码登录成功后，控制台应明确使用新登录 bot，或至少提示用户当前仍在旧 bot 上；过期 bot 的监听失败也应可见。

**实际行为**：`/login` 只新增 bot 和启动监听，不切换已有 active bot；发送仍使用旧 bot，因此远端返回 `session timeout`。监听侧 getupdates 失败被静默吞掉，用户只能看到“收不到消息”。

## 4. 环境信息

- 涉及模块 / 功能：控制台 `/login`、bot active session、消息监听、iLink 发送。
- 相关文件 / 函数：`internal/console/console.go:65`、`internal/app/app.go:207`、`internal/app/app.go:230`。
- 运行环境：本地 CLI / systemd control console 均可能触发。
- 其他上下文：用户提供了登录成功与发送失败日志；未读取本地 auth store，避免暴露 bot token。

## 5. 严重程度

**P1** — 核心收发链路受损，且错误表现容易误导用户以为新登录 bot 已生效；可通过手动 `/bots` 或删除旧 bot 临时绕过。

## 备注

临时绕过：执行 `/bots` 查看列表并选择新登录的 `3b4a...`，或删除旧的 `e927...` bot 后重新登录。
