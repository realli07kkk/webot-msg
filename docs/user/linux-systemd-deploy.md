---
doc_type: user-guide
slug: linux-systemd-deploy
component: 2026-06-10-linux-systemd-deploy
status: current
summary: 说明如何在 Linux systemd 环境安装、升级和控制 webot-msg 服务
tags: [deploy, linux, systemd, control-console]
last_reviewed: 2026-06-11
---

# Linux systemd 部署说明

## 功能简介

`scripts/linux-service.sh` 用于在 Linux systemd 环境从源码目录安装和升级 `webot-msg`。脚本会编译仓库内的 Go 程序，把二进制安装到 `/usr/local/bin/webot-msg`，准备默认运行目录和配置文件，并生成 `webot-msg.service`。

安装不会自动启动服务。确认配置后，可以通过脚本或 `systemctl` 启动。

## 前置条件

- 目标机器使用 Linux，并且当前系统由 `systemd` 管理。
- 已安装 Go，并且版本满足 `go.mod` 中的要求。
- 执行用户可以通过 `sudo` 写入 `/etc/systemd/system/` 并执行 `systemctl`。
- 脚本需要从仓库根目录或仓库内路径执行。

## 安装

```bash
./scripts/linux-service.sh install
```

安装动作会执行：

- 编译当前源码到 `bin/webot-msg`
- 安装二进制到 `/usr/local/bin/webot-msg`
- 创建 `~/.webot-msg/config/` 和 `~/.webot-msg/logs/`
- 首次写入 `~/.webot-msg/config/webot-msg.toml`
- 写入 `/etc/systemd/system/webot-msg.service`
- 执行 `systemctl daemon-reload`

默认配置文件已存在时，脚本会保留原文件，不会覆盖端口、日志路径、iLink 地址等用户改动。升级时如果旧配置缺少 `[redis]` section，脚本会非破坏性追加默认 Redis 配置，方便首次开启保护时执行 `/protection enable`。脚本不会删除或修改 `~/.webot-msg/config/auth.json`。

安装完成后，`webot-msg` 位于常见系统 `PATH` 内，可以直接确认：

```bash
which webot-msg
```

## 启动与控制

可以直接使用 `systemctl`：

```bash
sudo systemctl start webot-msg
sudo systemctl stop webot-msg
sudo systemctl restart webot-msg
sudo systemctl status webot-msg
```

也可以使用脚本透传：

```bash
./scripts/linux-service.sh start
./scripts/linux-service.sh stop
./scripts/linux-service.sh restart
./scripts/linux-service.sh status
```

service 固定读取 `~/.webot-msg/config/webot-msg.toml`，`ExecStart` 不再传配置路径参数，例如：

```text
ExecStart=/usr/local/bin/webot-msg
```

进入 systemd 服务的控制台时，不要再直接启动一个新的 `webot-msg` 实例。应连接正在运行服务暴露的本地 socket：

```bash
webot-msg console
```

在控制台里执行 `/login` 可以扫码添加 bot。`/exit` 或 `/quit` 只退出这次控制台连接，服务继续运行。真正停止进程仍使用：

```bash
./scripts/linux-service.sh stop
```

`webot-msg console` 在本地 stdin/stdout 都是 TTY 时支持按键级 Tab 补全，候选与直接前台控制台一致，例如 `/pro<Tab>` 会补成 `/protection `。为兼容已运行的旧 service，client 仍只在按 Enter 后向 socket 发送整行文本，不发送额外协议头。通过管道执行命令时仍使用 line mode 行输入，不提供按键级补全，例如 `printf '/exit\n' | webot-msg console`。

## 升级

```bash
./scripts/linux-service.sh upgrade
```

升级动作会先用 `systemctl is-active` 判断 `webot-msg` 是否正在运行：

- 如果服务处于 `active`，脚本会先 `stop`，再编译 `bin/webot-msg`、替换 `/usr/local/bin/webot-msg`，刷新 `/etc/systemd/system/webot-msg.service`，执行 `daemon-reload`，成功后重新 `start`
- 如果服务不是 `active`，脚本会编译 `bin/webot-msg`、替换 `/usr/local/bin/webot-msg`，刷新 systemd unit 并 `daemon-reload`，但不会自动启动

编译会先写入临时二进制，成功后才替换 `bin/webot-msg`，再安装到 `/usr/local/bin/webot-msg`。如果 `go build` 失败，旧二进制不会被破坏。

升级不会覆盖已有 `~/.webot-msg/config/webot-msg.toml` 的既有字段，也不会把原本关闭的发送保护自动打开。已经通过 `/protection enable` 成功开启过的实例，会在升级重启后读取 `~/.webot-msg/state/protection.json` 并尝试一次自动恢复保护；如果 Redis 不可用，服务仍会启动但保护保持关闭，并在日志中提示可修复后手动执行 `/protection enable`。首次开启保护时，如果旧配置缺少 `[redis]` section，脚本会追加默认 Redis 配置；确认 Redis 地址和密码后，进入 `webot-msg console` 执行 `/protection enable`。

## 默认配置

首次安装写入的配置内容如下：

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

Runtime config 只保存启动参数，不要写入 bot token、API token、context token 或消息正文。开启发送保护模式时，`redis.password` 属于本机凭据，不要提交到 Git。

## 相关功能

- [运行配置说明](./runtime-config.md)
- [本地登录微信 bot 并回复最近会话](../../.codestable/requirements/bot-message-bridge.md)
