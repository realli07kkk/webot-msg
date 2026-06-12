---
doc_type: user-guide
slug: runtime-config
component: 2026-06-10-runtime-toml-config
status: current
summary: 说明如何用 TOML 配置 webot-msg 的端口、凭据路径、控制台 socket、iLink 地址、日志文件策略和 Redis
tags: [config, cli, control-console, autocomplete, logging, protection, redis]
last_reviewed: 2026-06-12
---

# 运行配置说明

## 功能简介

`webot-msg` 启动时默认读取 `~/.webot-msg/config/webot-msg.toml`，用来调整本地 API 端口、auth store 路径、本地控制台 socket、iLink BaseURL、日志文件路径、日志大小上限和 Redis 连接。

如果默认配置文件不存在，程序会回退到内置默认值，保持直接运行二进制的兼容性。CLI 不再提供启动参数覆盖入口；要调整端口、auth store 或 control socket，请修改默认 TOML 后重启服务。无参运行 `webot-msg` 时，如果配置里的 control socket 已有运行中服务，程序会直接接入该控制台；没有可用 socket 时才启动新的前台 service。

## 前置条件

- 你已经能运行 `go run ./cmd/webot-msg`，已经构建出 `./bin/webot-msg`，或已经通过部署脚本安装出系统命令 `webot-msg`。
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

[control]
socket_path = "~/.webot-msg/webot-msg.sock"

[ilink]
base_url = "https://ilinkai.weixin.qq.com"

[log]
file_path = "~/.webot-msg/logs/webot-msg.log"
max_size = "100MB"

[redis]
url = "redis://localhost:6379/0"
password = ""
key_prefix = "webot-msg"
```

3. 启动或接入服务：

```bash
go run ./cmd/webot-msg
```

构建后启动：

```bash
./bin/webot-msg
```

通过 Linux 部署脚本安装后，也可以直接启动系统命令：

```bash
webot-msg
```

如果对应 control socket 已有运行中服务，以上命令会接入已有服务的控制台；如果没有可用服务，则启动新的前台 service。

4. 调整端口或路径时，修改 `~/.webot-msg/config/webot-msg.toml` 后重启服务。`webot-msg` 只接受无参启动，配置路径固定为默认路径。

## 配置项

| 配置项 | 默认值 | 怎么调 |
|---|---|---|
| `api.port` | `26322` | 改成 `1` 到 `65535` 之间的端口；修改后重启服务生效 |
| `storage.auth_path` | `~/.webot-msg/config/auth.json` | 改成 auth store JSON 文件路径；支持 `~` 开头的 home 路径 |
| `control.socket_path` | `~/.webot-msg/webot-msg.sock` | 本地控制台连接正在运行服务的 Unix socket 路径；支持 `~` 开头的 home 路径 |
| `ilink.base_url` | `https://ilinkai.weixin.qq.com` | 只接受 `http://` 或 `https://` 地址，必须包含 host |
| `log.file_path` | `~/.webot-msg/logs/webot-msg.log` | 改成标准日志输出文件路径；设为空字符串可以关闭文件日志 |
| `log.max_size` | `100MB` | 支持 `B`、`KB`、`MB`、`GB`、`TB`，大小写不敏感，例如 `"10MB"`、`"1GB"` |
| `redis.url` | `""`（部署脚本示例写 `redis://localhost:6379/0`） | Redis 地址和 DB；执行 `/protection enable` 时不能为空，推荐不在 URL 中写密码 |
| `redis.password` | `""` | Redis 认证密码；如果 `redis.url` 已自带 password，本字段也非空会在执行 `/protection enable` 时失败 |
| `redis.key_prefix` | `webot-msg` | Redis key 前缀；不同环境共用同一个 Redis 时建议改成不同值 |

## 发送保护模式

发送保护模式默认关闭，并且开启状态不写在 TOML 里。需要启用时，先配置 `[redis]`，再进入运行中服务的控制台执行：

```text
/protection enable
```

开启后，程序会把保护开关写入 `~/.webot-msg/state/protection.json`。服务重启或升级后会读取这个状态文件；如果记录为开启，程序会在启动时尝试一次自动恢复保护。Redis 不可用时恢复失败并告警，保护保持关闭，状态文件不会被改写，你可以修复 Redis 后手动执行 `/protection enable`。

保护开启后，程序使用 Redis 按 bot 记录最近一次微信 app 主动对话后的下发次数和 24h 窗口。

- 下发次数快达到内置限制时，程序会用最后的下发额度发送保护提醒，随后冻结普通文本发送。
- 24h 主动对话窗口快结束时，程序也会发送提醒并冻结普通文本发送。
- 冻结后，你需要从微信 app 给机器人主动发一条消息；监听到这条消息后，程序会清零计数、重置 24h 窗口并解除冻结。
- 保护状态按 bot 分开存储，内置规则全局共用；一个 bot 冻结不会影响另一个 bot。
- Redis 不可用、认证失败或保护状态读写失败时，保护模式会 fail closed，拒绝普通文本发送，避免静默越过限制。
- 查看当前 active bot 离触发限制还剩多少次数或时间，可以执行 `/protection status`。
- 关闭保护可以执行 `/protection disable`；关闭状态也会写入状态文件，服务重启后保持关闭。

`redis.password` 不会写入日志。建议把带密码的真实配置文件留在部署机器本地，不提交到 Git。

## 默认路径与旧文件迁移

默认路径如下：

```text
~/.webot-msg/
  config/
    auth.json
    webot-msg.toml
  logs/
    webot-msg.log
  state/
    protection.json
  webot-msg.sock
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
- Runtime config 可以提交模板，但不要把真实凭据写进去，尤其不要提交真实 `redis.password`。

## 常见问题

Q: 可以在启动时指向另一份配置吗？

A: 不可以。当前入口固定读取 `~/.webot-msg/config/webot-msg.toml`。需要换配置时，把内容写入默认路径后重启服务。

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

A: 当前 CLI 不支持临时端口覆盖。修改 TOML 的 `api.port` 后重启服务。

Q: systemd 启动后怎么进入控制台？

A: 直接运行同一个用户下的 `webot-msg`。程序会读取默认 TOML，发现配置里的 control socket 已有运行中服务后，接入该服务的控制台，而不是再启动第二个实例：

```bash
webot-msg
```

如果需要排查 socket 或使用第三方工具，也可以连接默认配置里的本地 control socket，例如默认路径：

```bash
socat - UNIX-CONNECT:"$HOME/.webot-msg/webot-msg.sock"
```

控制台内 `/exit` 或 `/quit` 只退出这次控制台连接，不会停止 systemd 服务。停止服务仍使用 `systemctl stop webot-msg` 或部署脚本的 `stop`。

直接前台启动 service 或用 `webot-msg` 在 TTY 中接入已有 service 时，控制台支持用 Tab 补全已声明命令和固定子命令，例如 `/log<Tab>` 补成 `/login`，`/pro<Tab>` 补成 `/protection `，`/protection st<Tab>` 补成 `/protection status`。前台 service 模式下按 Ctrl+C 会保存配置并退出进程；接入已有 service 时，`/exit`、`/quit` 或 Ctrl+C 只关闭本次控制台连接。

通过 `socat` / `nc -U` 连接 control socket 时按普通 line mode 输入命令，不提供按键级 Tab 补全。

## 相关功能

- [Linux systemd 部署说明](./linux-systemd-deploy.md)
- [本地登录微信 bot 并回复最近会话](../../.codestable/requirements/bot-message-bridge.md)
- [架构总入口](../../.codestable/architecture/ARCHITECTURE.md)
