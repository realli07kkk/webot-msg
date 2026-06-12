---
doc_type: issue-fix
issue: 2026-06-12-deploy-missing-telemetry-config
path: fast-track
fix_date: 2026-06-12
tags: [deploy, config, telemetry, docs]
---

# 部署脚本缺少 telemetry 默认配置修复记录

## 1. 问题描述

`otel-tracing` 功能已新增 `[telemetry]` Runtime config，但 Linux systemd 部署脚本仍只写旧默认 TOML。首次安装不会生成 `[telemetry]` section，升级旧配置时也不会非破坏性补上该 section。用户文档中的运行配置和部署默认配置同样未展示 telemetry 配置项。

## 2. 根因

feature 实现只覆盖了 runtimeconfig、main、API、iLink 和 OpenTelemetry 初始化挂载点，验收时没有把 `scripts/linux-service.sh` 的默认配置模板和升级补 section 逻辑纳入核对范围。由于 `[telemetry]` 缺失时程序默认关闭，问题不会导致启动失败，但部署体验和文档与新配置能力不一致。

## 3. 修复方案

在部署脚本中补 `write_default_telemetry_config`，首次安装写入 endpoint 为空的默认关闭 telemetry 配置；升级已有配置时，如果没有任何 `[telemetry]` 相关 section，则非破坏性追加默认关闭配置。同步更新运行配置说明、Linux systemd 部署说明和架构文档。

## 4. 改动文件清单

- `scripts/linux-service.sh`：新增 telemetry 默认配置写入和升级补全逻辑。
- `docs/user/runtime-config.md`：补充 telemetry 配置示例、字段表、安全说明。
- `docs/user/linux-systemd-deploy.md`：补充部署脚本默认配置和升级行为说明。
- `.codestable/architecture/ARCHITECTURE.md`：同步部署脚本对 `[telemetry]` section 的非破坏性补全行为。

## 5. 验证结果

- `bash -n scripts/linux-service.sh`：通过。
- `go test ./...`：通过。
- `go vet ./...`：通过。
- `git diff --check`：通过。

## 6. 遗留事项

无。本次只修 telemetry 配置模板遗漏；不改已有 Redis 默认密码策略。
