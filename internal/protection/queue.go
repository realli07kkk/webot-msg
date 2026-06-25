package protection

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type queuedPayload struct {
	Text       string `json:"text"`
	EnqueuedMs int64  `json:"enqueued_ms"`
}

type SendQueueController interface {
	AcquireOrEnqueue(ctx context.Context, botID string, text string) (Ingress, error)
	PeekQueued(ctx context.Context, botID string) (text string, enqueuedMs int64, ok bool, err error)
	DropFront(ctx context.Context, botID string) error
	QueueLen(ctx context.Context, botID string) (int, error)
}

type IngressOutcome int

const (
	IngressSendNow IngressOutcome = iota
	IngressQueued
	IngressQueueFull
)

type Ingress struct {
	Outcome      IngressOutcome
	Reservation  Reservation
	QueueLen     int
	SendReminder bool
	Reason       string
}

func (g *RedisGuard) AcquireOrEnqueue(ctx context.Context, botID string, text string) (Ingress, error) {
	payload, err := json.Marshal(queuedPayload{
		Text:       text,
		EnqueuedMs: g.now().UnixMilli(),
	})
	if err != nil {
		return Ingress{}, err
	}

	threshold := g.messageLimit - g.messageWarningRemaining
	res, err := acquireOrEnqueueScript.Run(ctx, g.client, g.queueKeys(botID),
		threshold,
		g.timeWarningBefore.Milliseconds(),
		string(payload),
		g.queueMaxLen,
		g.queueTTL.Milliseconds(),
	).Result()
	if err != nil {
		return Ingress{}, NewRejection("redis", err)
	}
	ingress, err := parseIngressResponse(res, threshold, g.timeWarningBefore)
	if err != nil {
		return Ingress{}, err
	}
	return ingress, nil
}

func (g *RedisGuard) PeekQueued(ctx context.Context, botID string) (string, int64, bool, error) {
	value, err := g.client.LIndex(ctx, g.queueKey(botID), 0).Result()
	if err == redis.Nil {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, NewRejection("redis", err)
	}

	var payload queuedPayload
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return "", 0, false, fmt.Errorf("decode queued payload: %w", err)
	}
	return payload.Text, payload.EnqueuedMs, true, nil
}

func (g *RedisGuard) DropFront(ctx context.Context, botID string) error {
	if err := g.client.LPop(ctx, g.queueKey(botID)).Err(); err != nil && err != redis.Nil {
		return NewRejection("redis", err)
	}
	return nil
}

func (g *RedisGuard) QueueLen(ctx context.Context, botID string) (int, error) {
	n, err := g.client.LLen(ctx, g.queueKey(botID)).Result()
	if err != nil {
		return 0, NewRejection("redis", err)
	}
	return int(n), nil
}

func (g *RedisGuard) queueKeys(botID string) []string {
	keys := g.keys(botID)
	return append(keys, g.queueKey(botID))
}

func (g *RedisGuard) queueKey(botID string) string {
	tag := "{" + botID + "}"
	return g.keyPrefix + ":protect:" + tag + ":queue"
}

func parseIngressResponse(res interface{}, threshold int, timeWarningBefore time.Duration) (Ingress, error) {
	values, ok := res.([]interface{})
	if !ok || len(values) < 2 {
		return Ingress{}, fmt.Errorf("unexpected ingress response: %v", res)
	}
	kind := fmt.Sprint(values[0])
	reason := fmt.Sprint(values[1])
	switch kind {
	case "send":
		reservation, err := reservationWithStatus(SendNormal(), values, threshold, timeWarningBefore)
		if err != nil {
			return Ingress{}, err
		}
		return Ingress{Outcome: IngressSendNow, Reservation: reservation, Reason: reason}, nil
	case "send_then_reminder":
		reservation, err := reservationWithStatus(SendNormalThenReminder(reason), values, threshold, timeWarningBefore)
		if err != nil {
			return Ingress{}, err
		}
		return Ingress{Outcome: IngressSendNow, Reservation: reservation, Reason: reason}, nil
	case "queued":
		queueLen, err := parseQueueLen(values)
		if err != nil {
			return Ingress{}, err
		}
		return Ingress{Outcome: IngressQueued, QueueLen: queueLen, Reason: reason}, nil
	case "queued_reminder":
		queueLen, err := parseQueueLen(values)
		if err != nil {
			return Ingress{}, err
		}
		return Ingress{Outcome: IngressQueued, QueueLen: queueLen, SendReminder: true, Reason: reason}, nil
	case "full":
		return Ingress{Outcome: IngressQueueFull, Reason: reason}, nil
	default:
		return Ingress{}, fmt.Errorf("unexpected ingress outcome %q", kind)
	}
}

func parseQueueLen(values []interface{}) (int, error) {
	if len(values) < 3 {
		return 0, fmt.Errorf("unexpected queue response: %v", values)
	}
	queueLen, err := parseScriptInt(values[2])
	if err != nil {
		return 0, fmt.Errorf("parse queue length failed: %w", err)
	}
	return queueLen, nil
}

var acquireOrEnqueueScript = redis.NewScript(`
local state = KEYS[1]
local active = KEYS[2]
local queue = KEYS[3]
local threshold = tonumber(ARGV[1])
local warn_ms = tonumber(ARGV[2])
local payload = ARGV[3]
local max_len = tonumber(ARGV[4])
local ttl_ms = tonumber(ARGV[5])

local function enqueue(reason, with_reminder)
  if redis.call("LLEN", queue) >= max_len then
    return {"full", reason or ""}
  end
  redis.call("RPUSH", queue, payload)
  redis.call("PEXPIRE", queue, ttl_ms)
  local queued = redis.call("LLEN", queue)
  if with_reminder == "1" then
    return {"queued_reminder", reason or "", queued}
  end
  return {"queued", reason or "", queued}
end

local frozen = redis.call("HGET", state, "frozen")
if frozen == "1" or redis.call("LLEN", queue) > 0 then
  return enqueue(redis.call("HGET", state, "reason") or "", "0")
end

local ttl = redis.call("PTTL", active)
if ttl == -2 or ttl == -1 then
  redis.call("HSET", state, "frozen", "1", "reason", "time", "reminder_pending", "0")
  return enqueue("time", "0")
end
if ttl <= warn_ms then
  redis.call("HSET", state, "frozen", "1", "reason", "time", "reminder_pending", "1")
  return enqueue("time", "1")
end

local count = tonumber(redis.call("HGET", state, "out_count") or "0")
count = count + 1
redis.call("HSET", state, "out_count", count)
if count >= threshold then
  redis.call("HSET", state, "frozen", "1", "reason", "count", "reminder_pending", "1")
  return {"send_then_reminder", "count", count, ttl}
end
return {"send", "", count, ttl}
`)

var _ SendQueueController = (*RedisGuard)(nil)
