---
doc_type: feature-acceptance
feature: 2026-06-10-linux-systemd-deploy
status: accepted
summary: Linux systemd 编译部署脚本已实现，架构与 requirement 已回写
tags: [deploy, linux, systemd, script]
accepted_at: 2026-06-10
---

# linux-systemd-deploy 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-10
> 关联方案 doc：`.codestable/features/2026-06-10-linux-systemd-deploy/linux-systemd-deploy-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `./scripts/linux-service.sh install`：入口存在于 `scripts/linux-service.sh:261`，串联依赖检查、构建、运行目录、默认 TOML、service unit 和 `daemon-reload`。
- [x] `./scripts/linux-service.sh upgrade`：入口存在于 `scripts/linux-service.sh:272`，先记录 active 状态，再按需 stop/build/start。
- [x] `./scripts/linux-service.sh start|stop|restart|status`：入口存在于 `scripts/linux-service.sh:298` 和 `main` 分发，透传到同名 `systemctl` 操作。

**名词层“现状 → 变化”逐项核对**：
- [x] Linux deploy script：新增 `scripts/linux-service.sh`，没有改 Go 主入口。
- [x] 默认 Runtime config 模板：`write_default_config` 写入与 `docs/user/runtime-config.md` 一致的默认 TOML，已有文件时保留。
- [x] Service unit：`write_service_unit` 生成 `webot-msg.service`，包含 `User`、`Group`、`WorkingDirectory`、`ExecStart` 和 restart 策略。
- [x] 部署路径变量：脚本解析 repo root、binary path、deploy user/home、config path、log dir 和 service path。
- [x] 升级状态记录：`cmd_upgrade` 在 stop 前记录 `was_active`，并只根据该值决定是否 start。

**流程图核对**：
- [x] install 图中节点均有实际落点：`prepare_common_context`、`build_binary`、`prepare_runtime_dirs`、`write_default_config`、`write_service_unit`、`daemon_reload`。
- [x] upgrade 图中节点均有实际落点：`service_is_active`、`stop_service`、`build_binary`、`start_service`。
- [x] service 控制节点有实际落点：`cmd_systemctl`。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 安装命令：脚本提供 `install`，构建 `bin/webot-msg`、准备 `~/.webot-msg` 子目录、写默认配置并生成 service。
- [x] 升级命令：脚本提供 `upgrade`，使用 `systemctl is-active --quiet webot-msg` 判断运行状态。
- [x] start/stop/restart/status：脚本透传到 `systemctl`，同时生成的 service 也支持用户直接用 `systemctl` 管理。

**明确不做逐项核对**：
- [x] 未引入 `.deb`、RPM、Homebrew、Docker、Ansible。
- [x] 未改变 Go CLI、Runtime config schema、HTTP API 或登录/监听业务流程；Go 文件无改动。
- [x] 脚本不包含 bot token、API token、context token 或消息内容。证据：`rg` 脚本无敏感字段命中。
- [x] 脚本不迁移、覆盖或删除 `~/.webot-msg/config/auth.json`。
- [x] 未实现多实例管理，service 名固定为 `webot-msg.service`。
- [x] 未实现健康检查、日志归档、备份、回滚或远程部署。

**关键决策落地**：
- [x] Bash 仓库脚本：落在 `scripts/linux-service.sh`。
- [x] 二进制保留在仓库 `bin/webot-msg`：`BINARY_PATH="${BIN_DIR}/${SERVICE_NAME}"`。
- [x] 默认配置路径在用户 home，service 使用绝对路径：`CONFIG_PATH="${DEPLOY_HOME}/.webot-msg/config/webot-msg.toml"`。
- [x] system service 写入 `/etc/systemd/system/webot-msg.service`，并通过 `User=` / `Group=` 指定部署用户运行。
- [x] 部署用户优先取 `SUDO_USER`，否则取当前用户。
- [x] 默认配置只在不存在时创建；已有配置保留。
- [x] `config/` 和 `.webot-msg/` 使用 owner-only，`logs/` 使用常规 owner 可写。
- [x] upgrade 只在原 active 时重启服务。
- [x] 构建先写临时二进制，成功后再替换。

**编排层“现状 → 变化”逐项核对**：
- [x] 构建命令从文档约定变成脚本编排：`build_binary`。
- [x] 默认 TOML 从用户手写变成 install 首次写入：`write_default_config`。
- [x] service 管理入口从手动拼 unit 变成脚本生成 unit + 控制子命令。

**流程级约束核对**：
- [x] 错误语义：脚本用 `fail` 非零退出并输出阶段；缺 Linux/systemd 在本机实测为 `error: Linux systemd environment required`。
- [x] 幂等性：`write_default_config` 遇到已有 TOML 直接保留；脚本无 auth 删除逻辑。
- [x] 顺序：`cmd_upgrade` 先 `service_is_active`，active 时 stop，再 build，最后按 `was_active` start。
- [x] 权限：写 systemd 和 `systemctl` 通过 `sudo_cmd`；service 用部署用户运行。
- [x] 可观测性：各阶段输出 `==>` 日志，不输出凭据。

**挂载点反向核对**：
- [x] 脚本入口：`install` / `upgrade` / `start` / `stop` / `restart` / `status` 均在 `main` 分发。
- [x] 默认配置文件：`CONFIG_PATH` 指向 `~/.webot-msg/config/webot-msg.toml`，由 `write_default_config` 使用。
- [x] systemd service：`SERVICE_FILE` 指向 `/etc/systemd/system/webot-msg.service`，由 `write_service_unit` 使用。
- [x] 构建产物：`BINARY_PATH` 指向仓库 `bin/webot-msg`，由 build 和 service unit 使用。
- [x] 用户文档：新增 `docs/user/linux-systemd-deploy.md`，并从 Runtime config 文档链接过去。
- [x] 反向 grep：`linux-service` / `webot-msg.service` / `systemd` 命中均落在脚本、部署文档、架构/需求归并和 feature spec 内。
- [x] 拔除沙盘推演：删除 `scripts/linux-service.sh`、`docs/user/linux-systemd-deploy.md`、Runtime config 文档链接、architecture/requirement 的部署段落后，该 feature 用户可见面会消失；auth store 与 Go 主流程不受影响。

## 3. 验收场景核对

- [x] S1：Linux/systemd 上执行 install 后生成二进制、目录、默认 TOML。
  - 证据来源：代码核对；当前环境非 Linux/systemd，未实机执行。
  - 结果：代码路径通过，目标环境实操待部署机验证。
- [x] S2：首次 install 默认 TOML 内容正确。
  - 证据来源：`write_default_config` 静态核对。
  - 结果：通过。
- [x] S3：已有 TOML 后 install 不覆盖，auth store 不删除。
  - 证据来源：`[[ -e "${CONFIG_PATH}" ]]` 早退；脚本无 `auth.json` 删除/写入逻辑。
  - 结果：通过。
- [x] S4：service `ExecStart` 使用绝对二进制路径和绝对配置路径。
  - 证据来源：`REPO_ROOT`、`BINARY_PATH`、`DEPLOY_HOME` 都解析为绝对路径；`write_service_unit` 静态核对。
  - 结果：通过。
- [x] S5：`systemctl start/stop/restart/status webot-msg` 可识别 service。
  - 证据来源：service unit 写入 `/etc/systemd/system/webot-msg.service` 并执行 `daemon-reload`；当前环境未实机执行。
  - 结果：代码路径通过，目标环境实操待部署机验证。
- [x] S6：脚本 `start/stop/restart/status` 透传。
  - 证据来源：`cmd_systemctl` 和 `main` 分发静态核对。
  - 结果：通过。
- [x] S7：服务 active 时 upgrade stop/build/start。
  - 证据来源：`cmd_upgrade` 静态核对；当前环境未实机执行。
  - 结果：代码路径通过，目标环境实操待部署机验证。
- [x] S8：服务 inactive 时 upgrade 只替换，不 start。
  - 证据来源：`cmd_upgrade` 静态核对。
  - 结果：通过。
- [x] S9：`go build` 失败不破坏旧二进制。
  - 证据来源：`build_binary` 先构建到 `mktemp`，失败时删除临时文件并退出，成功后 `mv` 替换。
  - 结果：通过。
- [x] S10：缺少依赖或权限时非零退出并指出阶段。
  - 证据来源：`require_command`、`require_linux_systemd`、`require_sudo`、各阶段 `fail`；本机 install 输出非 Linux/systemd 错误。
  - 结果：通过。

## 4. 术语一致性

- Linux deploy script：代码/文档统一落为 `scripts/linux-service.sh`。
- Install：脚本子命令为 `install`，文档和 checklist 一致。
- Upgrade：脚本子命令为 `upgrade`，文档和 checklist 一致。
- Runtime config：默认配置内容沿用现有 TOML 契约，没有新增配置 key。
- Service unit：文档和架构中统一称 `webot-msg.service`。
- 防冲突：未新增与现有 Go 类型冲突的类型或包名；Go 代码未改。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md`：已新增 Linux deploy script 与 Service unit 术语。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在“结构与交互”记录 `install` / `upgrade` / service 控制编排。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在“数据与状态”记录默认 Runtime config 文件由脚本首次写入，auth store 仍由 `config.Store` 管理。
- [x] `.codestable/architecture/ARCHITECTURE.md`：已在“关键决策”和“已知约束”记录 Linux/systemd 单实例、非包管理器、多实例不做、升级只恢复原 active 状态。

## 6. requirement 回写

- [x] `requirement=bot-message-bridge` 指向 current req，本次新增用户可见部署能力，已实际更新 `.codestable/requirements/bot-message-bridge.md`。
- [x] `.codestable/requirements/VISION.md` 已同步 pitch。

## 7. roadmap 回写

- [x] 非 roadmap 起头：design frontmatter 无 `roadmap` / `roadmap_item` 字段，跳过 roadmap 回写。

## 8. attention.md 候选盘点

- [ ] 候选 1：本项目的 Linux systemd 部署脚本无法在 macOS/非 systemd 环境完整验收；修改 `scripts/linux-service.sh` 后需要在目标 Linux systemd 主机补跑 `install` / `upgrade` / `start` / `stop` / `restart` / `status`。

## 9. 遗留

- 后续优化点：无。
- 已知限制：真实 Linux systemd 主机上的 service install/upgrade/start/stop/restart/status 尚未在当前环境执行；当前验收基于代码核对、静态检查和非 Linux 失败路径验证。
- 实现阶段顺手发现：无。
