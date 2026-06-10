---
doc_type: issue-fix
issue: 2026-06-10-stale-active-bot-session
path: standard
fix_date: 2026-06-10
related: [stale-active-bot-session-analysis.md]
tags: [console, bot-session, ilink, messaging]
---

# 登录后仍使用旧 Bot 导致 Session Timeout 修复记录

## 1. 实际采用方案

采用分析文档中的方案 C：

- `/login` 成功后总是把新登录 bot 设为当前控制台 active bot，并输出切换提示。
- `monitorWeixin` 对 `GetUpdates` 错误做节流日志，避免 session 过期时完全静默。
- `persistUpdateState` 保存最近消息上下文时，同时保存 `FromUserID` 到 `IlinkUserID`，让回复目标和 `ContextToken` 成对更新。

## 2. 改动文件清单

- `internal/console/console.go` — 修复 `/login` 后保留旧 active bot 的状态问题。
- `internal/console/console_test.go` — 增加已有 active bot 时登录后发送使用新 bot 的测试。
- `internal/app/app.go` — 增加监听错误节流日志，并让最近消息目标和上下文成对持久化。
- `internal/app/app_test.go` — 增加 `persistUpdateState` 同步 `IlinkUserID`、`ContextToken` 和 `GetUpdatesBuf` 的测试。
- `.codestable/issues/2026-06-10-stale-active-bot-session/stale-active-bot-session-analysis.md` — 标记用户已确认方案。

## 3. 验证结果

- `go build ./...`：通过。
- `go test ./...`：通过。
- `go vet ./...`：通过。
- 复现步骤验证：用单元测试覆盖“已有旧 active bot -> `/login` 新 bot -> 普通文本发送”路径，发送目标从旧 bot 变为新 bot。
- 影响面回归：控制台已有 `/bot` 会话本地切换测试仍覆盖；新增 app 测试覆盖消息监听持久化路径。

## 4. 遗留事项

- 未连接真实 iLink 环境做端到端扫码发送验证；需要在运行服务中重新 `/login` 后确认提示符切换到新 bot，再等待一条消息并发送回复。
- 监听错误现在写标准日志；如果后续发现同类错误需要更明确的用户提示，可另开 issue 设计控制台可见状态。
