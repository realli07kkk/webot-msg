---
doc_type: requirement
slug: bot-message-bridge
pitch: 在本地登录微信 bot 后，用控制台或受保护 API 回复最近会话，并可通过 TOML 与 Linux systemd 脚本管理本地运行
status: current
last_reviewed: 2026-06-10
implemented_by:
  - architecture-overview
tags: [bot, messaging, local-api, config, deploy, systemd]
---

# 本地登录微信 bot 并回复最近会话

## 用户故事

- 作为需要调试 bot 回复的人，我希望扫码登录后能直接在终端发送文本，而不是每次都手动拼远端请求。
- 作为要把 bot 接到自动化流程的人，我希望有一个本地 HTTP 入口发送消息，而不是让外部流程直接持有远端 bot 凭证。
- 作为同时维护多个 bot 的人，我希望能查看、切换和删除已登录 bot，而不是改本地配置文件。
- 作为本地部署或调试工具的人，我希望能用 TOML 调整 API 端口、auth store 路径、iLink BaseURL 和日志文件策略，而不是改代码或依赖工作目录里的固定路径。
- 作为用 systemd 部署服务的人，我希望能进入正在运行服务的控制台执行 `/login`，退出控制台时服务继续运行，而不是再启动一个新实例。
- 作为在 Linux 机器上部署本工具的人，我希望能用脚本完成编译、默认配置落盘和 systemd service 安装/升级，而不是手动拼 service 文件和升级顺序。

## 为什么需要

bot 登录态、消息上下文、发送入口、本地运行参数和 Linux 部署动作分散处理时，很容易把凭证、会话上下文、调试动作和部署差异混在一起。这个项目把它们收束到一个本地工具里，让开发者可以先登录 bot、等到消息上下文就绪，再从控制台或受保护 API 发出回复；运行参数通过独立 TOML 配置表达，Linux systemd 部署通过仓库脚本编排，不混入凭据文件。

## 怎么解决

用户扫码登录后，工具在本地保存可复用的 bot 配置，持续接收消息并记录最近可回复的会话上下文。需要发送时，用户可以在控制台输入文本，也可以让外部流程调用本地受保护入口，由工具代为把文本回复到当前上下文。systemd 部署下，用户通过 `webot-msg console` 连接运行中服务的本地 Unix socket，控制台退出不会停止 service。

启动时，工具默认读取 `~/.webot-msg/config/webot-msg.toml`，配置 API 端口、auth store 路径、本地控制台 socket、iLink BaseURL、日志文件路径和日志大小上限；默认配置不存在时回退内置默认值。默认 auth store、日志文件与 control socket 统一落在 `~/.webot-msg/` 下，auth store JSON schema 不变。

在 Linux systemd 环境中，用户可以用 `scripts/linux-service.sh install` 编译 `bin/webot-msg`、安装 `/usr/local/bin/webot-msg`、创建 `~/.webot-msg/config/` 和 `~/.webot-msg/logs/`、首次写入默认 `webot-msg.toml`，并生成 `webot-msg.service`。升级时，脚本先按 `systemctl is-active` 记录服务是否运行：运行中则 stop、替换系统 PATH 中的二进制后再 start；未运行则只替换二进制。

## 边界

- 它只处理文本回复和 typing 状态，不负责富媒体消息编排。
- 它依赖最近消息提供可回复上下文；没有上下文时不能主动创建新会话。
- 它是本地运行工具，不提供多用户权限系统或公网部署安全边界。
- 它不替用户管理微信侧 bot 的生命周期，只保存本地登录和调用所需信息。
- Runtime config 不保存 bot token、API token、context token 或消息上下文；这些仍属于 auth store。
- 配置入口仅包含默认 `~/.webot-msg/config/webot-msg.toml`、兼容 `-c`、兼容 `-port` 和 `serve` / `console` 命令，不提供环境变量配置入口。
- `webot-msg console` 只连接本机 Unix socket，不提供公网远程管理通道。
- 日志配置只提供文件路径和大小上限，不提供按时间切割、压缩归档、保留天数或远程日志采集。
- 默认 auth store 按本地凭据处理，目录和文件使用 owner-only 权限；显式自定义路径的挂载、备份和系统权限由部署者负责。
- Linux 部署脚本只面向 systemd 单实例，不提供 `.deb`、RPM、Docker、Ansible、多实例管理、备份或回滚。
- 安装脚本不会覆盖已有 `~/.webot-msg/config/webot-msg.toml`，也不会删除或修改 `~/.webot-msg/config/auth.json`；服务操作需要部署者具备 sudo 权限。
- 安装脚本固定把用户可执行命令安装到 `/usr/local/bin/webot-msg`，不提供安装前缀配置。

## 变更记录

- 2026-06-10：新增 TOML Runtime config 能力，覆盖 API 端口、auth store 路径、iLink BaseURL、日志文件路径和大小上限；默认存储迁移到 `~/.webot-msg/`，并保留旧 `./config/auth.json` 到新默认路径的一次性复制兼容。
- 2026-06-10：新增 Linux systemd 部署脚本，支持 `install`、`upgrade` 和 `start` / `stop` / `restart` / `status` 服务控制；安装首次写入默认 Runtime config，升级只恢复原本 active 的服务。
- 2026-06-10：新增 `webot-msg console` 本地控制台入口，通过 Unix socket 进入 systemd 管理的运行中服务；`/exit` 和 `/quit` 只退出控制台连接，停止进程仍由 systemd 管理。
- 2026-06-10：服务和控制台默认读取 `~/.webot-msg/config/webot-msg.toml`，同时保留 `-c` 兼容入口，避免破坏旧脚本。
- 2026-06-10：Linux 部署脚本新增把二进制安装到 `/usr/local/bin/webot-msg`，让 `which webot-msg` 和 `webot-msg console` 在部署后直接可用。
