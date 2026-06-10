---
doc_type: user-guide
slug: runtime-config
component: 2026-06-10-runtime-toml-config
status: current
summary: 说明如何用 TOML 配置 webot-msg 的端口、凭据路径、iLink 地址和日志文件策略
tags: [config, cli, logging]
last_reviewed: 2026-06-10
---

# 运行配置说明

## 功能简介

`webot-msg` 启动时可以通过 `-c` 读取 TOML 配置文件，用来调整本地 API 端口、auth store 路径、iLink BaseURL、日志文件路径和日志大小上限。

不传 `-c` 时，程序不会尝试读取默认配置文件，而是直接使用内置默认值。默认数据统一放在 `~/.webot-msg/` 下。

## 前置条件

- 你已经能运行 `go run ./cmd/webot-msg`，或已经构建出 `./bin/webot-msg`。
- 如果要复用旧登录态，旧文件仍在 `./config/auth.json`。
- Runtime config 只放启动参数，不要把 bot token、API token 或消息内容写进 TOML。

## 如何使用

1. 创建配置目录：

```bash
mkdir -p ~/.webot-msg/config
```

2. 写入配置文件 `~/.webot-msg/config/webot-msg.toml`：

```toml
[api]
port = 26322

[storage]
auth_path = "~/.webot-msg/config/auth.json"

[ilink]
base_url = "https://ilinkai.weixin.qq.com"

[log]
file_path = "~/.webot-msg/logs/webot-msg.log"
max_size = "100MB"
```

3. 使用配置文件启动：

```bash
go run ./cmd/webot-msg -c ~/.webot-msg/config/webot-msg.toml
```

构建后启动：

```bash
./bin/webot-msg -c ~/.webot-msg/config/webot-msg.toml
```

4. 临时覆盖端口：

```bash
./bin/webot-msg -c ~/.webot-msg/config/webot-msg.toml -port 8080
```

同时传 `-c` 和显式 `-port` 时，`-port` 优先生效。

## 配置项

| 配置项 | 默认值 | 怎么调 |
|---|---|---|
| `api.port` | `26322` | 改成 `1` 到 `65535` 之间的端口；也可以启动时用 `-port` 临时覆盖 |
| `storage.auth_path` | `~/.webot-msg/config/auth.json` | 改成 auth store JSON 文件路径；支持 `~` 开头的 home 路径 |
| `ilink.base_url` | `https://ilinkai.weixin.qq.com` | 只接受 `http://` 或 `https://` 地址，必须包含 host |
| `log.file_path` | `~/.webot-msg/logs/webot-msg.log` | 改成标准日志输出文件路径；设为空字符串可以关闭文件日志 |
| `log.max_size` | `100MB` | 支持 `B`、`KB`、`MB`、`GB`、`TB`，大小写不敏感，例如 `"10MB"`、`"1GB"` |

## 默认路径与旧文件迁移

默认路径如下：

```text
~/.webot-msg/
  config/
    auth.json
    webot-msg.toml
  logs/
    webot-msg.log
```

升级后，如果你没有显式配置 `storage.auth_path`，并且旧的 `./config/auth.json` 存在、新的 `~/.webot-msg/config/auth.json` 不存在，程序启动时会把旧文件原样复制到新默认路径。

这个复制只发生一次：新默认 auth 文件已经存在时，不会再用旧文件覆盖它。旧路径也不会和新路径保持同步。

## 日志文件

文件日志只接管标准日志输出，不会把控制台交互提示、二维码或收到的完整消息内容复制进日志文件。

当日志文件下一次写入会超过 `log.max_size` 时，当前日志会被重命名为 `.1` 备份，新日志文件从空文件继续写入。当前实现只保留一个 `.1` 备份，不支持保留天数、压缩归档或远程采集。

## 安全注意事项

- auth store 保存 bot token、API token 和消息上下文，不要提交到 Git。
- 默认 auth store 目录和文件会按 owner-only 权限创建。
- 自定义 `storage.auth_path` 时，建议仍放在当前用户私有目录下。
- Runtime config 可以提交模板，但不要把真实凭据写进去。

## 常见问题

Q: 不传 `-c` 会自动读取 `~/.webot-msg/config/webot-msg.toml` 吗？

A: 不会。不传 `-c` 时只使用内置默认值。要读取 TOML，必须显式传 `-c`。

Q: 配置里写错字段名会怎样？

A: 程序会启动失败并提示 unknown runtime config key。比如 `[storage] authpath = "..."` 是错的，正确字段是 `auth_path`。

Q: `ilink.base_url = "ftp://example.com"` 可以吗？

A: 不可以。`ilink.base_url` 只接受 `http` 或 `https`。

Q: 想继续使用旧的 `./config/auth.json` 怎么办？

A: 在 TOML 里显式配置：

```toml
[storage]
auth_path = "./config/auth.json"
```

这样程序会直接使用旧路径，不触发默认迁移。

Q: 怎么只临时换端口，不改 TOML？

A: 启动时传 `-port`：

```bash
./bin/webot-msg -c ~/.webot-msg/config/webot-msg.toml -port 19090
```

## 相关功能

- [Linux systemd 部署说明](./linux-systemd-deploy.md)
- [本地登录微信 bot 并回复最近会话](../../.codestable/requirements/bot-message-bridge.md)
- [架构总入口](../../.codestable/architecture/ARCHITECTURE.md)
