---
doc_type: issue-fix
issue: 2026-06-13-telemetry-resource-metadata
path: fast-track
fix_date: 2026-06-13
tags: [telemetry, tracing, opentelemetry, apm]
---

# Telemetry Resource Metadata 修复记录

## 1. 问题描述

启用 OpenTelemetry traces 后，上报到 APM 的 trace 数据里 `service.instance` 和 `agent.version` 等元数据为 `unknown`。用户期望这些字段由程序自动带上对应实例和 SDK / 探针元数据，而不是要求每个部署手工在 `[telemetry.resource_attributes]` 里补。

## 2. 根因

`internal/telemetry.newResource` 只构造了 `service.name` 和 TOML 里的自由 `resource_attributes`，没有合并 OpenTelemetry SDK 默认元数据，也没有采集 `host.name` 和服务实例 ID。腾讯云 APM 的 `service.instance` 来自 Resource 的 `host.name`，因此缺失时会显示为 `unknown`；`agent.version` 也没有任何自动填充值。

## 3. 修复方案

Telemetry 初始化 Resource 时合并以下自动元数据：

- `host.name`：由 OpenTelemetry Go SDK host detector 读取，用于 APM 映射 `service.instance`。
- `service.instance.id`：每次进程启动生成一个服务实例 ID。
- `telemetry.sdk.name` / `telemetry.sdk.language` / `telemetry.sdk.version`：由 OpenTelemetry Go SDK metadata detector 提供。
- `agent.version`：填当前 OpenTelemetry Go SDK 版本，补齐 APM 的 agent version 展示字段。

继续保留 TOML `telemetry.service_name` 和 `telemetry.resource_attributes`；手工 resource attribute 仍可覆盖自动值。不引入环境变量配置入口。

## 4. 改动文件清单

- `internal/telemetry/telemetry.go`：`newResource` 合并 host、service、SDK metadata detectors，并自动补 `agent.version`。
- `internal/telemetry/telemetry_test.go`：补断言确认 OTLP export resource 包含 `host.name`、`service.instance.id`、SDK metadata 和 `agent.version`。

## 5. 验证结果

- `go test ./internal/telemetry`：通过。
- `go test ./...`：通过。
- `go vet ./...`：通过。

## 6. 遗留事项

无。本次只修自动上报 resource metadata，不改变 telemetry 配置来源和 exporter 行为。
