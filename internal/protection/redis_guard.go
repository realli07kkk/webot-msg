package protection

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisGuardConfig struct {
	KeyPrefix               string
	MessageLimit            int
	MessageWarningRemaining int
	ActiveWindow            time.Duration
	TimeWarningBefore       time.Duration
}

type RedisGuard struct {
	client                  *redis.Client
	keyPrefix               string
	messageLimit            int
	messageWarningRemaining int
	activeWindow            time.Duration
	timeWarningBefore       time.Duration
	now                     func() time.Time
}

func NewRedisClient(rawURL string, password string) (*redis.Client, error) {
	rawURL, err := ValidateRedisURL(rawURL, password)
	if err != nil {
		return nil, err
	}
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url failed")
	}
	if password != "" {
		opts.Password = password
	}
	return redis.NewClient(opts), nil
}

func ValidateRedisURL(value string, password string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("redis.url: must not be empty")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("redis.url: invalid URL")
	}
	if parsed.Scheme != "redis" && parsed.Scheme != "rediss" {
		return "", fmt.Errorf("redis.url: scheme must be redis or rediss")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("redis.url: host must not be empty")
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword && password != "" {
			return "", fmt.Errorf("redis.password: must not be set when redis.url already contains a password")
		}
	}
	return strings.TrimRight(value, "/"), nil
}

func NewRedisGuard(client *redis.Client, cfg RedisGuardConfig) *RedisGuard {
	return &RedisGuard{
		client:                  client,
		keyPrefix:               cfg.KeyPrefix,
		messageLimit:            cfg.MessageLimit,
		messageWarningRemaining: cfg.MessageWarningRemaining,
		activeWindow:            cfg.ActiveWindow,
		timeWarningBefore:       cfg.TimeWarningBefore,
		now:                     time.Now,
	}
}

func (g *RedisGuard) ReserveNormalSend(ctx context.Context, botID string) (Reservation, error) {
	threshold := g.messageLimit - g.messageWarningRemaining
	reservation, err := runReservationScript(ctx, reserveNormalSendScript, g.client, g.keys(botID), threshold, g.timeWarningBefore.Milliseconds())
	if err != nil {
		return Reservation{}, NewRejection("redis", err)
	}
	return reservation, nil
}

func (g *RedisGuard) ReleaseNormalSend(ctx context.Context, botID string) error {
	if err := releaseNormalSendScript.Run(ctx, g.client, g.keys(botID)).Err(); err != nil {
		return NewRejection("redis", err)
	}
	return nil
}

func (g *RedisGuard) RecordReminderSend(ctx context.Context, botID string) error {
	keys := g.keys(botID)
	nowMs := g.now().UnixMilli()
	if err := recordReminderSendScript.Run(ctx, g.client, keys, nowMs).Err(); err != nil {
		return NewRejection("redis", err)
	}
	return nil
}

func (g *RedisGuard) RecordActiveConversation(ctx context.Context, botID string) error {
	keys := g.keys(botID)
	nowMs := g.now().UnixMilli()
	if err := recordActiveConversationScript.Run(ctx, g.client, keys, nowMs, g.activeWindow.Milliseconds()).Err(); err != nil {
		return NewRejection("redis", err)
	}
	return nil
}

func (g *RedisGuard) CheckTimeWindow(ctx context.Context, botID string) (Decision, error) {
	decision, err := runDecisionScript(ctx, checkTimeWindowScript, g.client, g.keys(botID), g.timeWarningBefore.Milliseconds())
	if err != nil {
		return Decision{}, NewRejection("redis", err)
	}
	return decision, nil
}

func (g *RedisGuard) ProtectionStatus(ctx context.Context, botID string) (Status, error) {
	keys := g.keys(botID)
	values, err := g.client.HMGet(ctx, keys[0], "out_count", "frozen", "reason", "reminder_pending").Result()
	if err != nil {
		return Status{}, NewRejection("redis", err)
	}
	ttl, err := g.client.PTTL(ctx, keys[1]).Result()
	if err != nil {
		return Status{}, NewRejection("redis", err)
	}

	outCount := parseRedisInt(values[0])
	threshold := g.messageLimit - g.messageWarningRemaining
	messagesBeforeReminder := threshold - outCount
	if messagesBeforeReminder < 0 {
		messagesBeforeReminder = 0
	}

	activeWindowRemaining := time.Duration(0)
	timeBeforeWarning := time.Duration(0)
	activeWindowReady := ttl > 0
	if activeWindowReady {
		activeWindowRemaining = ttl
		timeBeforeWarning = ttl - g.timeWarningBefore
		if timeBeforeWarning < 0 {
			timeBeforeWarning = 0
		}
	}

	return Status{
		Enabled:                true,
		BotID:                  botID,
		ActiveWindowReady:      activeWindowReady,
		Frozen:                 fmt.Sprint(values[1]) == "1",
		Reason:                 fmt.Sprint(values[2]),
		OutCount:               outCount,
		MessageLimit:           g.messageLimit,
		WarningThreshold:       threshold,
		MessagesBeforeReminder: messagesBeforeReminder,
		ActiveWindowRemaining:  activeWindowRemaining,
		TimeBeforeWarning:      timeBeforeWarning,
		ReminderPending:        fmt.Sprint(values[3]) == "1",
	}, nil
}

func parseRedisInt(value interface{}) int {
	if value == nil {
		return 0
	}
	n, err := strconv.Atoi(fmt.Sprint(value))
	if err != nil {
		return 0
	}
	return n
}

func (g *RedisGuard) keys(botID string) []string {
	tag := "{" + botID + "}"
	base := g.keyPrefix + ":protect:" + tag
	return []string{base + ":state", base + ":active"}
}

func runDecisionScript(ctx context.Context, script *redis.Script, client *redis.Client, keys []string, args ...interface{}) (Decision, error) {
	res, err := script.Run(ctx, client, keys, args...).Result()
	if err != nil {
		return Decision{}, err
	}
	values, ok := res.([]interface{})
	if !ok || len(values) < 2 {
		return Decision{}, fmt.Errorf("unexpected script response: %v", res)
	}
	kind := fmt.Sprint(values[0])
	reason := fmt.Sprint(values[1])
	switch kind {
	case "allow":
		return Allow(), nil
	case "reject":
		return Reject(reason), nil
	case "reminder":
		return SendReminderAndFreeze(reason), nil
	default:
		return Decision{}, fmt.Errorf("unexpected decision %q", kind)
	}
}

func runReservationScript(ctx context.Context, script *redis.Script, client *redis.Client, keys []string, args ...interface{}) (Reservation, error) {
	res, err := script.Run(ctx, client, keys, args...).Result()
	if err != nil {
		return Reservation{}, err
	}
	values, ok := res.([]interface{})
	if !ok || len(values) < 2 {
		return Reservation{}, fmt.Errorf("unexpected script response: %v", res)
	}
	kind := fmt.Sprint(values[0])
	reason := fmt.Sprint(values[1])
	switch kind {
	case "send":
		return SendNormal(), nil
	case "reject":
		return RejectNormal(reason), nil
	case "send_then_reminder":
		return SendNormalThenReminder(reason), nil
	case "reminder_only":
		return SendReminderOnly(reason), nil
	default:
		return Reservation{}, fmt.Errorf("unexpected reservation %q", kind)
	}
}

func (g *RedisGuard) StateKey(botID string) string {
	return g.keys(botID)[0]
}

func (g *RedisGuard) ActiveKey(botID string) string {
	return g.keys(botID)[1]
}

var reserveNormalSendScript = redis.NewScript(`
local state = KEYS[1]
local active = KEYS[2]
local threshold = tonumber(ARGV[1])
local warn_ms = tonumber(ARGV[2])

local frozen = redis.call("HGET", state, "frozen")
if frozen == "1" then
  return {"reject", redis.call("HGET", state, "reason") or ""}
end

local ttl = redis.call("PTTL", active)
if ttl == -2 or ttl == -1 then
  redis.call("HSET", state, "frozen", "1", "reason", "time", "reminder_pending", "0")
  return {"reject", "time"}
end
if ttl <= warn_ms then
  redis.call("HSET", state, "frozen", "1", "reason", "time", "reminder_pending", "1")
  return {"reminder_only", "time"}
end

local count = tonumber(redis.call("HGET", state, "out_count") or "0")
count = count + 1
redis.call("HSET", state, "out_count", count)
if count >= threshold then
  redis.call("HSET", state, "frozen", "1", "reason", "count", "reminder_pending", "1")
  return {"send_then_reminder", "count"}
end
return {"send", ""}
`)

var releaseNormalSendScript = redis.NewScript(`
local state = KEYS[1]

local count = tonumber(redis.call("HGET", state, "out_count") or "0")
if count > 0 then
  count = redis.call("HINCRBY", state, "out_count", -1)
end

local reason = redis.call("HGET", state, "reason")
local pending = redis.call("HGET", state, "reminder_pending")
if reason == "count" and pending == "1" then
  redis.call("HSET", state, "frozen", "0", "reason", "", "reminder_pending", "0")
end
return count
`)

var checkTimeWindowScript = redis.NewScript(`
local state = KEYS[1]
local active = KEYS[2]
local warn_ms = tonumber(ARGV[1])

local frozen = redis.call("HGET", state, "frozen")
if frozen == "1" then
  return {"reject", redis.call("HGET", state, "reason") or ""}
end

local ttl = redis.call("PTTL", active)
if ttl == -2 or ttl == -1 then
  return {"reject", "time"}
end
if ttl <= warn_ms then
  redis.call("HSET", state, "frozen", "1", "reason", "time", "reminder_pending", "1")
  return {"reminder", "time"}
end
return {"allow", ""}
`)

var recordReminderSendScript = redis.NewScript(`
local state = KEYS[1]
local now_ms = ARGV[1]

redis.call("HINCRBY", state, "out_count", 1)
redis.call("HSET", state, "frozen", "1", "reminder_pending", "0", "reminder_sent_ms", now_ms)
return "OK"
`)

var recordActiveConversationScript = redis.NewScript(`
local state = KEYS[1]
local active = KEYS[2]
local now_ms = ARGV[1]
local active_window_ms = tonumber(ARGV[2])

redis.call("HSET", state,
  "out_count", "0",
  "frozen", "0",
  "reason", "",
  "reminder_pending", "0",
  "last_active_ms", now_ms
)
redis.call("SET", active, "1", "PX", active_window_ms)
return "OK"
`)
