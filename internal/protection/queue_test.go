package protection

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisGuardAcquireOrEnqueueSendsNowWhenQueueEmpty(t *testing.T) {
	guard, _, closeClient := newQueueTestRedisGuard(t, RedisGuardConfig{})
	defer closeClient()

	if err := guard.RecordActiveConversation(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RecordActiveConversation() error = %v", err)
	}

	ingress, err := guard.AcquireOrEnqueue(context.Background(), "bot-1", "hello")
	if err != nil {
		t.Fatalf("AcquireOrEnqueue() error = %v", err)
	}
	if ingress.Outcome != IngressSendNow || ingress.Reservation.Kind != ReservationSendNormal {
		t.Fatalf("AcquireOrEnqueue() = %#v, want send now normal", ingress)
	}
	if !ingress.Reservation.HasStatus {
		t.Fatal("Reservation.HasStatus = false, want true")
	}
	got, err := guard.QueueLen(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("QueueLen() error = %v", err)
	}
	if got != 0 {
		t.Fatalf("QueueLen() = %d, want 0", got)
	}
}

func TestRedisGuardAcquireOrEnqueueReturnsSendThenReminderAtCountThreshold(t *testing.T) {
	guard, _, closeClient := newQueueTestRedisGuard(t, RedisGuardConfig{})
	defer closeClient()

	if err := guard.RecordActiveConversation(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RecordActiveConversation() error = %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := guard.ReserveNormalSend(context.Background(), "bot-1"); err != nil {
			t.Fatalf("ReserveNormalSend(%d) error = %v", i+1, err)
		}
	}

	ingress, err := guard.AcquireOrEnqueue(context.Background(), "bot-1", "critical")
	if err != nil {
		t.Fatalf("AcquireOrEnqueue() error = %v", err)
	}
	if ingress.Outcome != IngressSendNow || ingress.Reservation.Kind != ReservationSendNormalThenReminder || ingress.Reason != ReasonCount {
		t.Fatalf("AcquireOrEnqueue() = %#v, want send then reminder count", ingress)
	}
	if ingress.Reservation.MessagesBeforeReminder != 0 {
		t.Fatalf("MessagesBeforeReminder = %d, want 0", ingress.Reservation.MessagesBeforeReminder)
	}
}

func TestRedisGuardAcquireOrEnqueueQueuesWhenFrozen(t *testing.T) {
	guard, _, closeClient := newQueueTestRedisGuard(t, RedisGuardConfig{})
	defer closeClient()

	if err := guard.RecordActiveConversation(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RecordActiveConversation() error = %v", err)
	}
	for i := 0; i < 9; i++ {
		if _, err := guard.ReserveNormalSend(context.Background(), "bot-1"); err != nil {
			t.Fatalf("ReserveNormalSend(%d) error = %v", i+1, err)
		}
	}

	ingress, err := guard.AcquireOrEnqueue(context.Background(), "bot-1", "queued")
	if err != nil {
		t.Fatalf("AcquireOrEnqueue() error = %v", err)
	}
	if ingress.Outcome != IngressQueued || ingress.QueueLen != 1 {
		t.Fatalf("AcquireOrEnqueue() = %#v, want queued length 1", ingress)
	}
	text, _, ok, err := guard.PeekQueued(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("PeekQueued() error = %v", err)
	}
	if !ok || text != "queued" {
		t.Fatalf("PeekQueued() = %q, ok=%v, want queued", text, ok)
	}
}

func TestRedisGuardAcquireOrEnqueueQueuesWhenBacklogExists(t *testing.T) {
	guard, redisServer, closeClient := newQueueTestRedisGuard(t, RedisGuardConfig{})
	defer closeClient()

	if err := guard.RecordActiveConversation(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RecordActiveConversation() error = %v", err)
	}
	pushQueuedPayload(t, guard, "bot-1", "backlog", time.Now().UnixMilli())

	ingress, err := guard.AcquireOrEnqueue(context.Background(), "bot-1", "after")
	if err != nil {
		t.Fatalf("AcquireOrEnqueue() error = %v", err)
	}
	if ingress.Outcome != IngressQueued || ingress.QueueLen != 2 {
		t.Fatalf("AcquireOrEnqueue() = %#v, want queued length 2", ingress)
	}
	stateKey := guard.StateKey("bot-1")
	if got := redisServer.HGet(stateKey, "out_count"); got != "0" {
		t.Fatalf("out_count = %q, want 0 when backlog forces queue", got)
	}
}

func TestRedisGuardAcquireOrEnqueueReturnsQueueFull(t *testing.T) {
	guard, _, closeClient := newQueueTestRedisGuard(t, RedisGuardConfig{
		QueueMaxLen: 2,
	})
	defer closeClient()

	for _, text := range []string{"one", "two"} {
		ingress, err := guard.AcquireOrEnqueue(context.Background(), "bot-1", text)
		if err != nil {
			t.Fatalf("AcquireOrEnqueue(%q) error = %v", text, err)
		}
		if ingress.Outcome != IngressQueued {
			t.Fatalf("AcquireOrEnqueue(%q) = %#v, want queued", text, ingress)
		}
	}

	ingress, err := guard.AcquireOrEnqueue(context.Background(), "bot-1", "three")
	if err != nil {
		t.Fatalf("AcquireOrEnqueue(full) error = %v", err)
	}
	if ingress.Outcome != IngressQueueFull {
		t.Fatalf("AcquireOrEnqueue(full) = %#v, want full", ingress)
	}
	if got, err := guard.QueueLen(context.Background(), "bot-1"); err != nil || got != 2 {
		t.Fatalf("QueueLen() = %d, %v, want 2, nil", got, err)
	}
}

func TestRedisGuardAcquireOrEnqueueQueuesTimeWarningWithReminder(t *testing.T) {
	guard, redisServer, closeClient := newQueueTestRedisGuard(t, RedisGuardConfig{})
	defer closeClient()

	if err := guard.RecordActiveConversation(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RecordActiveConversation() error = %v", err)
	}
	guard.client.Do(context.Background(), "PEXPIRE", guard.ActiveKey("bot-1"), int64((29 * time.Minute).Milliseconds()))

	ingress, err := guard.AcquireOrEnqueue(context.Background(), "bot-1", "late")
	if err != nil {
		t.Fatalf("AcquireOrEnqueue() error = %v", err)
	}
	if ingress.Outcome != IngressQueued || !ingress.SendReminder || ingress.Reason != ReasonTime || ingress.QueueLen != 1 {
		t.Fatalf("AcquireOrEnqueue() = %#v, want queued reminder time", ingress)
	}
	stateKey := guard.StateKey("bot-1")
	if got := redisServer.HGet(stateKey, "frozen"); got != "1" {
		t.Fatalf("frozen = %q, want 1", got)
	}
	if got := redisServer.HGet(stateKey, "reminder_pending"); got != "1" {
		t.Fatalf("reminder_pending = %q, want 1", got)
	}
}

func TestRedisGuardAcquireOrEnqueueQueuesMissingActiveWithoutReminder(t *testing.T) {
	guard, redisServer, closeClient := newQueueTestRedisGuard(t, RedisGuardConfig{})
	defer closeClient()

	ingress, err := guard.AcquireOrEnqueue(context.Background(), "bot-1", "wait")
	if err != nil {
		t.Fatalf("AcquireOrEnqueue() error = %v", err)
	}
	if ingress.Outcome != IngressQueued || ingress.SendReminder || ingress.Reason != ReasonTime || ingress.QueueLen != 1 {
		t.Fatalf("AcquireOrEnqueue() = %#v, want queued time without reminder", ingress)
	}
	stateKey := guard.StateKey("bot-1")
	if got := redisServer.HGet(stateKey, "reminder_pending"); got != "0" {
		t.Fatalf("reminder_pending = %q, want 0", got)
	}
}

func TestRedisGuardQueuePeekDropFIFOAndTTL(t *testing.T) {
	guard, redisServer, closeClient := newQueueTestRedisGuard(t, RedisGuardConfig{
		QueueTTL: 2 * time.Hour,
	})
	defer closeClient()

	nowMs := int64(1700000000000)
	pushQueuedPayload(t, guard, "bot-1", "first", nowMs)
	pushQueuedPayload(t, guard, "bot-1", "second", nowMs+1)

	text, enqueuedMs, ok, err := guard.PeekQueued(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("PeekQueued() error = %v", err)
	}
	if !ok || text != "first" || enqueuedMs != nowMs {
		t.Fatalf("PeekQueued() = %q, %d, %v, want first/%d/true", text, enqueuedMs, ok, nowMs)
	}
	if err := guard.DropFront(context.Background(), "bot-1"); err != nil {
		t.Fatalf("DropFront() error = %v", err)
	}
	text, _, ok, err = guard.PeekQueued(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("PeekQueued() second error = %v", err)
	}
	if !ok || text != "second" {
		t.Fatalf("PeekQueued() second = %q, ok=%v, want second", text, ok)
	}

	ingress, err := guard.AcquireOrEnqueue(context.Background(), "bot-1", "third")
	if err != nil {
		t.Fatalf("AcquireOrEnqueue() error = %v", err)
	}
	if ingress.Outcome != IngressQueued || ingress.QueueLen != 2 {
		t.Fatalf("AcquireOrEnqueue() = %#v, want queued length 2", ingress)
	}
	if ttl := redisServer.TTL(guard.queueKey("bot-1")); ttl <= 0 || ttl > 2*time.Hour {
		t.Fatalf("queue TTL = %s, want within 2h", ttl)
	}
}

func TestRedisGuardQueueLenAndProtectionStatusQueuedCount(t *testing.T) {
	guard, _, closeClient := newQueueTestRedisGuard(t, RedisGuardConfig{})
	defer closeClient()

	for _, text := range []string{"one", "two"} {
		if _, err := guard.AcquireOrEnqueue(context.Background(), "bot-1", text); err != nil {
			t.Fatalf("AcquireOrEnqueue(%q) error = %v", text, err)
		}
	}
	queueLen, err := guard.QueueLen(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("QueueLen() error = %v", err)
	}
	if queueLen != 2 {
		t.Fatalf("QueueLen() = %d, want 2", queueLen)
	}
	status, err := guard.ProtectionStatus(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("ProtectionStatus() error = %v", err)
	}
	if status.QueuedCount != 2 {
		t.Fatalf("Status.QueuedCount = %d, want 2", status.QueuedCount)
	}
}

func pushQueuedPayload(t *testing.T, guard *RedisGuard, botID string, text string, enqueuedMs int64) {
	t.Helper()

	payload, err := json.Marshal(queuedPayload{Text: text, EnqueuedMs: enqueuedMs})
	if err != nil {
		t.Fatalf("Marshal queuedPayload error = %v", err)
	}
	if err := guard.client.RPush(context.Background(), guard.queueKey(botID), string(payload)).Err(); err != nil {
		t.Fatalf("RPush queued payload error = %v", err)
	}
}

func newQueueTestRedisGuard(t *testing.T, cfg RedisGuardConfig) (*RedisGuard, *miniredis.Miniredis, func()) {
	t.Helper()

	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = "webot-msg"
	}
	if cfg.MessageLimit == 0 {
		cfg.MessageLimit = 10
	}
	if cfg.MessageWarningRemaining == 0 {
		cfg.MessageWarningRemaining = 1
	}
	if cfg.ActiveWindow == 0 {
		cfg.ActiveWindow = 24 * time.Hour
	}
	if cfg.TimeWarningBefore == 0 {
		cfg.TimeWarningBefore = 30 * time.Minute
	}
	guard := NewRedisGuard(client, cfg)
	return guard, redisServer, func() {
		client.Close()
		redisServer.Close()
	}
}
