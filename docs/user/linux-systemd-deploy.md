---
doc_type: user-guide
slug: linux-systemd-deploy
component: 2026-06-10-linux-systemd-deploy
status: current
summary: 说明如何在 Linux systemd 环境安装、升级和控制 webot-msg 服务
tags: [deploy, linux, systemd]
last_reviewed: 2026-06-10
---

# Linux systemd 部署说明

## 功能简介

`scripts/linux-service.sh` 用于在 Linux systemd 环境从源码目录安装和升级 `webot-msg`。脚本会编译仓库内的 Go 程序，准备默认运行目录和配置文件，并生成 `webot-msg.service`。

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
- 创建 `~/.webot-msg/config/` 和 `~/.webot-msg/logs/`
- 首次写入 `~/.webot-msg/config/webot-msg.toml`
- 写入 `/etc/systemd/system/webot-msg.service`
- 执行 `systemctl daemon-reload`

默认配置文件已存在时，脚本会保留原文件，不会覆盖端口、日志路径、iLink 地址等用户改动。脚本也不会删除或修改 `~/.webot-msg/config/auth.json`。

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

service 使用 `-c` 指向安装时解析出的默认配置文件绝对路径，例如：

```text
ExecStart=/path/to/repo/bin/webot-msg -c /home/deploy/.webot-msg/config/webot-msg.toml
```

## 升级

```bash
./scripts/linux-service.sh upgrade
```

升级动作会先用 `systemctl is-active` 判断 `webot-msg` 是否正在运行：

- 如果服务处于 `active`，脚本会先 `stop`，再编译替换 `bin/webot-msg`，成功后重新 `start`
- 如果服务不是 `active`，脚本只编译替换 `bin/webot-msg`，不会自动启动

编译会先写入临时二进制，成功后才替换 `bin/webot-msg`。如果 `go build` 失败，旧二进制不会被破坏。

## 默认配置

首次安装写入的配置内容如下：

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

Runtime config 只保存启动参数，不要写入 bot token、API token、context token 或消息正文。

## 相关功能

- [运行配置说明](./runtime-config.md)
- [本地登录微信 bot 并回复最近会话](../../.codestable/requirements/bot-message-bridge.md)
