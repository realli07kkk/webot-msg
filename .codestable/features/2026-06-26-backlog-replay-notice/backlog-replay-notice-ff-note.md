---
doc_type: feature-ff-note
feature: backlog-replay-notice
date: 2026-06-26
requirement: bot-message-bridge
tags: [protection, queue, replay, message-body]
---

## 做了什么
Redis 发送队列恢复重放时，补发正文最前面会加入收到 API 调用的原始时间，并说明该消息因发送保护积压而延迟补发；时间固定按 `Asia/Shanghai` 展示。

## 改了哪些
- `internal/app/send_queue.go:23` — 新增积压补发提示构造，并在 `drainSendQueue` 调用发送前拼到原文前面。
- `internal/app/send_queue_test.go:18` — 更新队列重放测试，并新增固定时间文案断言。
- `docs/user/runtime-config.md:172` — 补充用户可见的补发正文变化。

## 怎么验证的
已运行 `go test ./internal/app ./internal/protection ./internal/sender ./internal/api`、`go test ./...`、`go vet ./...` 和 `git diff --check`，均通过。
