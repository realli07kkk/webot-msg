---
doc_type: feature-ff-note
feature: systemd-control-console
date: 2026-06-10
requirement: bot-message-bridge
tags: [cli, control-console, systemd, config]
---

## 做了什么

新增 `webot-msg console`，通过本地 Unix socket 进入 systemd 管理的运行中服务；控制台内 `/exit` 和 `/quit` 只退出当前控制台会话，服务继续运行。服务和控制台默认读取 `~/.webot-msg/config/webot-msg.toml`，同时保留 `-c` 兼容入口。

## 改了哪些

- `cmd/webot-msg/main.go` — 增加 `serve` / `console` 命令分流，默认加载 Runtime config 路径，保留 `-c` 兼容，并在默认配置缺失时回退内置默认值。
- `internal/control/` — 新增 Unix socket server/client，把运行中服务的控制台暴露给本地 CLI。
- `internal/console/console.go`、`internal/app/app.go`、`internal/ilink/client.go` — 控制台改为基于 `io.Reader` / `io.Writer`，二维码和命令输出能回到附着会话，active bot 改为 session-local。
- `internal/runtimeconfig/config.go`、`internal/control/server.go`、`scripts/linux-service.sh`、`docs/user/`、`.codestable/` — 同步默认配置路径、control socket 契约、安全 socket 清理和 `upgrade` 每次刷新 systemd unit。

## 怎么验证的

已运行 `go test ./...` 和 `go vet ./...`。本地冒烟使用临时 `HOME` 默认配置路径启动服务，确认 `webot-msg console` 可连接，`/exit` 后服务仍存活；测试覆盖 `-c` 兼容、默认配置缺失 fallback、普通文件占用 socket path 不会被删除、自定义 socket 父目录权限不被 chmod、active bot 会话本地化。

## 顺手发现

无。
