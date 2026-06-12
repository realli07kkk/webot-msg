---
doc_type: issue-fix
issue: 2026-06-12-startup-error-hidden
path: fast-track
fix_date: 2026-06-12
tags: [cli, startup, logging, stderr]
---

# 启动错误被日志文件隐藏修复记录

## 1. 问题描述

用户在 Rocky Linux root shell 中执行 `webot-msg`，终端只输出：

```text
Loaded 1 bots.
Auto selected single bot: 293e11097e63@im.bot
```

随后进程回到 shell，没有进入前台 console，也没有在终端显示失败原因。

## 2. 根因

`cmd/webot-msg/main.go` 在初始化日志文件后调用 `log.SetOutput(logWriter)`。如果后续 `application.Run` 返回启动错误，例如默认 control socket 已被正在运行的 service 占用，`log.Fatal(err)` 只写入 `~/.webot-msg/logs/webot-msg.log`，不会写 stderr。

因此用户终端只看到 `app.Run` 内部在失败点之前已经打印的 `Loaded ...` / `Auto selected ...`，看不到真实错误：

```text
start control console failed: control socket already in use: ~/.webot-msg/webot-msg.sock
```

## 3. 修复方案

在 `cmd/webot-msg/main.go` 增加 `fatalStartupError`。当文件日志已接管标准 `log` 输出时，`application.Run` 的启动错误先写 stderr，再交给 `log.Fatal` 写入日志文件并退出；未启用文件日志时保持原有 `log.Fatal` 行为，避免重复输出。

## 4. 改动文件清单

- `cmd/webot-msg/main.go`：`application.Run` 错误路径改为 `fatalStartupError(err, logWriter != nil)`。
- `cmd/webot-msg/main_test.go`：新增回归测试，占用默认 control socket 后运行子进程 `main()`，断言 stderr 直接包含启动失败原因。

## 5. 验证结果

- 复现场景验证：先启动一个占用默认 control socket 的 service，再运行第二个 `webot-msg`，现在 stderr 输出 `start control console failed: control socket already in use: ...`。
- `go test ./cmd/webot-msg`：通过。
- `go test ./...`：通过。
- `go vet ./...`：通过。
- `git diff --check`：通过。

## 6. 遗留事项

- 无代码遗留。
- 如果用户想进入已经运行的 service 的控制台，当前 CLI 设计仍是不恢复 `webot-msg console`；应使用 `nc -U ~/.webot-msg/webot-msg.sock` 或 `socat - UNIX-CONNECT:"$HOME/.webot-msg/webot-msg.sock"` 连接。
