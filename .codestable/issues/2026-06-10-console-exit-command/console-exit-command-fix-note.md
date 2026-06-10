---
doc_type: issue-fix
issue: 2026-06-10-console-exit-command
path: fast-track
fix_date: 2026-06-10
tags: [cli, console, shutdown]
---

# 交互控制台缺少退出命令修复记录

## 1. 问题描述

用户通过 `webot-msg login` 完成二维码扫码后进入交互控制台。控制台只展示 `/login`、`/bots`、`/bot <num>`、`/del <num>` 和文本发送能力，没有提供显式退出命令；用户只能用 `Ctrl+C` 结束进程。

## 2. 根因

`internal/console/console.go` 的命令分支没有处理 `/exit` 或 `/quit`，并且 `Run` 原本不返回退出原因。`internal/app/app.go` 在 `console.Run` 返回后默认进入后台常驻，因此也无法区分“stdin 关闭”和“用户主动退出”。

## 3. 修复方案

为控制台增加 `ExitReason`，让 `/exit` 和 `/quit` 返回主动退出原因；`App.Run` 在收到主动退出原因后先保存配置再返回。stdin 关闭仍保持原行为，服务继续后台运行。

## 4. 改动文件清单

- `internal/console/console.go`：新增 `/exit`、`/quit` 命令和退出原因返回值。
- `internal/app/app.go`：处理主动退出，保存配置后结束运行。
- `internal/console/console_test.go`：覆盖 `/exit` 不会被当成消息发送，以及 stdin 关闭仍返回后台运行语义。
- `.codestable/architecture/ARCHITECTURE.md`：同步控制台命令和关闭路径的当前系统地图。

## 5. 验证结果

- `go test ./...`：通过。
- `go vet ./...`：通过。
- 单元验证：`internal/console` 覆盖 `/exit` 和 `/quit` 返回主动退出原因，且不会被当成普通文本消息发送。
- 回归验证：stdin 关闭仍返回 `ExitReasonInputClosed`，`App.Run` 保持原有“控制台不可用时后台运行”的行为。

## 6. 遗留事项

`webot-msg login` 中的 `login` 目前不是正式子命令，现有 `main` 只解析 flag 并忽略额外参数。如果目标是“一次性扫码登录后自动退出”的子命令，需要另开 feature 或 issue 明确定义 CLI 子命令语义。
