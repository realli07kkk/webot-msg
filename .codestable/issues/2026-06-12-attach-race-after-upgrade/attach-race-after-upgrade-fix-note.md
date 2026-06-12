---
doc_type: issue-fix
issue: 2026-06-12-attach-race-after-upgrade
path: fast-track
fix_date: 2026-06-12
tags: [cli, control-socket, systemd]
---

# upgrade 后无参入口仍可能报 socket already in use 修复记录

## 1. 问题描述

用户执行 `scripts/linux-service.sh upgrade` 后立即运行 `webot-msg`，仍看到：

```text
Loaded 1 bots.
Auto selected single bot: 293e11097e63@im.bot
start control console failed: control socket already in use: /root/.webot-msg/webot-msg.sock
```

期望行为是接入已经存在的 control socket，而不是尝试启动第二个 service。

## 2. 根因

上一版只在 `main()` 早期尝试 attach 一次；如果那次 attach 因 systemd service 正在启动、socket 短暂不可接入或其他时序原因返回“没有可用 live socket”，流程会继续进入 `app.Run`。随后 `control.Server.Start` 再次检查 socket 时可能已经发现原 service 的 socket 可用，于是返回 `control socket already in use`，但该错误没有被 `main()` 识别为“应转去 attach”的信号，最终变成 fatal。

## 3. 修复方案

- `internal/control` 为 live socket 占用场景增加 `ErrSocketAlreadyInUse` sentinel，并在 `listenUnixSocket` 中用 `%w` 包装。
- `cmd/webot-msg/main.go` 在 `app.Run` 返回该 sentinel 时，再次调用 `attachExistingConsole` 接入已有 service；只有 attach 失败或仍无可用 socket 时才保留原错误退出。
- `internal/control/server_test.go` 增加 sentinel 匹配测试，避免该错误再次退化成只能靠字符串判断。

## 4. 改动文件清单

- `cmd/webot-msg/main.go`
- `internal/control/server.go`
- `internal/control/server_test.go`

## 5. 验证结果

- 已执行 `go test ./cmd/webot-msg`，通过。
- 已执行 `go test ./internal/control`，通过。
- 已执行 `go test ./...`，通过。
- 已执行 `go vet ./...`，通过。
- 已执行 `git diff --check`，通过。
- 已执行 `.codestable/tools/validate-yaml.py` 校验本 fix-note，通过。

## 6. 遗留事项

无。
