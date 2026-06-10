---
doc_type: issue-fix
issue: 2026-06-10-control-console-message-broadcast
path: standard
fix_date: 2026-06-10
related: [control-console-message-broadcast-analysis.md]
tags: [control-console, messaging, systemd, output]
---

# Control Console 看不到监听消息修复记录

## 1. 实际采用方案

采用方案 A：control socket 连接注册为消息输出订阅者，监听协程收到消息时同时写服务 stdout 和所有活跃 control console。

## 2. 改动文件清单

- `internal/control/server.go` — 为每个 control socket 连接注册控制台输出，并用同步 writer 串行化命令输出与广播输出。
- `internal/control/server_test.go` — 覆盖输出注册和同步 writer 基本行为。
- `internal/app/app.go` — 增加 control console 输出注册表和广播逻辑，`printMessages` 广播渲染后的消息。
- `internal/app/app_test.go` — 覆盖注册控制台输出能收到消息、注销后不再收到消息。

## 3. 验证结果

- `go build ./...`：通过。
- `go test ./...`：通过。
- `go vet ./...`：通过。

## 4. 遗留事项

- 未连接真实 iLink/systemd 环境做端到端验证；部署后需要 `webot-msg console` 保持连接，从微信侧发消息确认当前控制台能直接显示。
- 当前广播给所有活跃 control console，不按每个控制台 active bot 过滤；如果后续需要多用户/多 bot 隔离，应另开 feature 设计订阅过滤。
