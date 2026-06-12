---
doc_type: feature-ff-note
feature: attach-existing-socket
date: 2026-06-12
requirement: bot-message-bridge
tags: [cli, console, socket, systemd]
---

# 无参入口接入已有 control socket

## 1. 目标

解决 systemd 服务已经运行时，用户再次执行 `webot-msg` 会尝试启动第二个服务并报 `control socket already in use` 的问题。

目标行为：

- `webot-msg` 仍然只接受无参运行，继续拒绝旧子命令和 flag。
- 默认 TOML 解析出 control socket 后，如果 socket 已有 live service，当前进程直接接入该控制台。
- 如果 socket 不存在、拒绝连接或是 stale socket，当前进程继续按原规则启动前台 service。
- TTY 下第一方 attach 保留本地行编辑和 Tab 补全；非 TTY 下保持管道 line mode。

## 2. 实现

- `cmd/webot-msg/main.go` 在 `buildRuntimeConfig` 后新增 `attachExistingConsole` 分流：优先尝试连接 `resolved.Control.SocketPath`，成功后直接返回；只有明确没有可用服务时才继续初始化存储、日志和 `app.Run`。
- `attachExistingConsole` 根据 stdin/stdout 是否都是 TTY 选择 `control.AttachInteractive` 或 `control.Attach`。
- 对 `os.ErrNotExist`、`ECONNREFUSED`、`ENOTSOCK`、`EPROTOTYPE` 这类“没有可接入 live socket”的错误回退到启动前台 service；其他错误继续暴露给用户。
- `cmd/webot-msg/main_test.go` 覆盖已有 socket attach 路径，并保留非 socket 占位文件导致启动失败且 stderr 可见的回归测试。

## 3. 文档回写

- `.codestable/architecture/ARCHITECTURE.md` 更新无参入口语义、control client helper 状态、systemd 交互决策和 Tab 补全边界。
- `.codestable/requirements/bot-message-bridge.md` 更新用户可见能力、边界和变更记录。
- `docs/user/runtime-config.md` 与 `docs/user/linux-systemd-deploy.md` 更新 systemd 后进入控制台的推荐方式：直接运行 `webot-msg`；`socat` / `nc -U` 作为第三方 fallback。

## 4. 验收

- 单测覆盖：有 live socket 时 `webot-msg` 直接 attach，不再启动 service；旧参数仍然非零退出；socket 路径被普通文件占用时错误写入 stderr。
- 已执行 `go test ./cmd/webot-msg`，通过。
- 已执行 `go test ./...`，通过。
- 已执行 `go vet ./...`，通过。
- 已执行 `git diff --check`，通过。
- 已执行 `.codestable/tools/validate-yaml.py` 校验本 ff-note frontmatter，通过。
