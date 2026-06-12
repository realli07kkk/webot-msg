---
doc_type: requirement
slug: service-observability
pitch: 让部署和运维者在兼容 APM 中看清本地 API 发送到外部调用的链路耗时，同时保持默认关闭和本地 TOML 配置边界
status: current
last_reviewed: 2026-06-12
implemented_by:
  - architecture-overview
tags: [observability, tracing, telemetry, operations, apm]
---

# 查看服务发送链路

## 用户故事

- 作为维护 webot-msg 的部署者，我希望在 APM 控制台看到一次本地 API 发送从入口到外部调用的耗时，而不是只能翻本地日志推断。
- 作为排查发送变慢的人，我希望确认慢在本地 API、外部 iLink 调用还是导出链路，而不是把整条发送路径当成黑盒。
- 作为需要接入不同监控厂商的人，我希望上报协议保持厂商中立，换厂商时只改本地运行配置，不改代码、不引入厂商 SDK。
- 作为管理本机凭据的人，我希望遥测不会把 bot token、API token、context token 或消息正文带进 span 或启动日志。

## 为什么需要

webot-msg 的核心发送链路跨过本地 HTTP API、保护编排和外部 iLink HTTP 调用。只靠本地日志时，用户能知道请求失败或成功，却难以直接判断一次发送的耗时分布，也无法在统一 APM 中和其他服务对齐排查。这个能力把本地 API 入站和外部调用串成同一条 trace，让部署者在需要时获得链路视角，同时不改变默认运行行为。

## 怎么解决

服务提供一个默认关闭的 telemetry 配置段。用户在本地 TOML 中配置 OTLP endpoint、协议、鉴权 header 和资源属性后，服务会把本地 API 请求和由该请求触发的 iLink 出站调用上报到兼容 OTLP 的 APM 或 collector。没有配置 endpoint 时，服务保持原有行为，不发起 OTLP 连接，也不要求用户准备监控组件。

配置只来自本地 TOML。即使运行环境里存在 OpenTelemetry 官方 exporter 支持的环境变量，本项目也不把它们作为本服务的遥测配置入口，避免部署环境隐式改变上报地址、header、TLS 或压缩策略。启动摘要可以记录 telemetry 是否启用及 endpoint、protocol、service name、insecure 状态，但不会打印 header 值或资源属性值。

## 边界

- 只覆盖 traces，不提供 metrics 或 logs 上报。
- 首期只追踪本地 API 入站和由该请求触发的 iLink 出站调用，不追踪后台长轮询、Redis 保护操作或控制台命令。
- 不提供采样率配置，首期按 OpenTelemetry SDK 默认处理已启用链路。
- 不引入厂商私有 SDK；用户需要自行准备兼容 OTLP 的 collector 或 APM endpoint。
- 遥测不替代业务错误处理；上报 endpoint 不可达时，发送请求本身仍按原业务路径返回。
- span 和 resource 不应包含消息正文、BotToken、APIToken 或 ContextToken；服务端 URL 属性不保留 query string。
- 运行配置入口仍是默认 `~/.webot-msg/config/webot-msg.toml`，不新增 CLI flag、子命令或环境变量配置入口。

## 变更记录

- 2026-06-12：新增可选 OpenTelemetry traces 能力，通过 OTLP 上报本地 API 入站和 iLink 出站链路；默认关闭，配置只来自 TOML。
