---
doc_type: issue-fix
issue: 2026-06-10-upgrade-missing-redis-config
path: fast-track
fix_date: 2026-06-10
tags: [deploy, systemd, config, redis]
---

# Upgrade 未给旧配置补 Redis Section 修复记录

## 1. 问题描述

用户在 Linux systemd 环境执行：

```bash
sh scripts/linux-service.sh upgrade
```

升级输出提示：

```text
config has no [redis] section; add Redis config before running /protection enable
```

但 `/root/.webot-msg/config/webot-msg.toml` 仍然没有 `[redis]` section，导致升级到控制台保护开关版本后，还需要手动编辑配置才能执行 `/protection enable`。

## 2. 根因

`scripts/linux-service.sh:cmd_upgrade` 在发现已有配置缺少 `[redis]` 时只打印提示，没有做非破坏性迁移。由于 `upgrade` 同时承诺保留已有配置、不覆盖用户改动，所以正确行为不是重写整份 TOML，而是在缺少 `[redis]` section 时追加默认 Redis 配置。

## 3. 修复方案

- 抽出 `write_default_redis_config`，让首次安装和升级补 section 共用同一份默认 Redis 配置。
- 新增 `ensure_redis_config_section`，仅当已有配置缺少 `[redis]` 时复制原文件、追加默认 `[redis]` section，再原子替换。
- 追加迁移会保留已有端口、日志路径、iLink 地址等字段，不覆盖已有配置。
- 同步用户部署文档和 architecture，把升级语义从“只提示补齐 Redis”改为“缺失时自动追加默认 Redis section”。

## 4. 改动文件清单

- `scripts/linux-service.sh` — 升级已有配置缺少 `[redis]` 时追加默认 Redis 配置。
- `docs/user/linux-systemd-deploy.md` — 更新升级行为说明。
- `.codestable/architecture/ARCHITECTURE.md` — 同步部署脚本配置迁移边界。

## 5. 验证结果

- `bash -n scripts/linux-service.sh`：通过。
- 临时配置模拟验证：旧 TOML 只有 `[api]` / `[storage]` 时，调用 `ensure_redis_config_section` 后追加一个 `[redis]` section；再次调用不会重复追加。
- `git diff --check`：通过。

## 6. 遗留事项

- 升级脚本只追加默认 Redis 配置，不会猜测生产 Redis 密码；如果部署 Redis 需要认证，用户仍需确认并填写 `redis.password` 后再执行 `/protection enable`。
