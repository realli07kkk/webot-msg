---
doc_type: requirement-index
slug: vision
status: current
last_reviewed: 2026-06-25
tags: []
---

# Requirements Vision

本目录记录 webot-msg 的能力愿景：用户需要什么、系统提供什么能力来满足，以及这些能力当前是否已经实现。

## Current

- [bot-message-bridge](bot-message-bridge.md) — 在本地登录微信 bot 后，用控制台或受保护 API 回复最近会话，保护冻结期 API 消息可等待恢复后按序补发，并可通过 TOML、发送保护、审计和 systemd 脚本管理本地运行。`status: current`
- [service-observability](service-observability.md) — 让部署和运维者在兼容 APM 中看清本地 API 发送到外部调用的链路耗时，同时保持默认关闭和本地 TOML 配置边界。`status: current`

## Draft

暂无。

## Outdated

暂无。
