package protection

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisGuardRecordActiveConversationResetsState(t *testing.T) {
	guard, redisServer, closeClient := newTestRedisGuard(t)
	defer closeClient()

	if err := guard.RecordActiveConversation(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RecordActiveConversation() error = %v", err)
	}

	stateKey := guard.StateKey("bot-1")
	if got := redisServer.HGet(stateKey, "out_count"); got != "0" {
		t.Fatalf("out_count = %q, want 0", got)
	}
	if got := redisServer.HGet(stateKey, "frozen"); got != "0" {
		t.Fatalf("frozen = %q, want 0", got)
	}
	if ttl := redisServer.TTL(guard.ActiveKey("bot-1")); ttl != 24*time.Hour {
		t.Fatalf("active TTL = %s, want 24h", ttl)
	}
}

func TestNewRedisClientUsesPasswordConfig(t *testing.T) {
	client, err := NewRedisClient("redis://localhost:6379/0", "secret")
	if err != nil {
		t.Fatalf("NewRedisClient() error = %v", err)
	}
	defer client.Close()

	if got := client.Options().Password; got != "secret" {
		t.Fatalf("client password = %q, want secret", got)
	}
}

func TestNewRedisClientRedactsParseErrors(t *testing.T) {
	_, err := NewRedisClient("redis://:secret-token@%zz", "")
	if err == nil {
		t.Fatal("NewRedisClient() error = nil, want parse error")
	}
	if got := err.Error(); got == "" || strings.Contains(got, "secret-token") {
		t.Fatalf("NewRedisClient() error = %q, must not contain redis password", got)
	}
}

func TestRedisGuardReserveNormalSendTriggersReminderAndCountsReminder(t *testing.T) {
	guard, redisServer, closeClient := newTestRedisGuard(t)
	defer closeClient()

	if err := guard.RecordActiveConversation(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RecordActiveConversation() error = %v", err)
	}
	for i := 0; i < 8; i++ {
		reservation, err := guard.ReserveNormalSend(context.Background(), "bot-1")
		if err != nil {
			t.Fatalf("ReserveNormalSend(%d) error = %v", i+1, err)
		}
		if reservation.Kind != ReservationSendNormal {
			t.Fatalf("ReserveNormalSend(%d) reservation = %#v, want send normal", i+1, reservation)
		}
	}

	reservation, err := guard.ReserveNormalSend(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("ReserveNormalSend(9) error = %v", err)
	}
	if reservation.Kind != ReservationSendNormalThenReminder || reservation.Reason != ReasonCount {
		t.Fatalf("ReserveNormalSend(9) reservation = %#v, want send normal then reminder count", reservation)
	}
	stateKey := guard.StateKey("bot-1")
	if got := redisServer.HGet(stateKey, "frozen"); got != "1" {
		t.Fatalf("frozen = %q, want 1", got)
	}
	if got := redisServer.HGet(stateKey, "out_count"); got != "9" {
		t.Fatalf("out_count before reminder = %q, want 9", got)
	}

	if err := guard.RecordReminderSend(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RecordReminderSend() error = %v", err)
	}
	if got := redisServer.HGet(stateKey, "out_count"); got != "10" {
		t.Fatalf("out_count after reminder = %q, want 10", got)
	}
	if got := redisServer.HGet(stateKey, "reminder_pending"); got != "0" {
		t.Fatalf("reminder_pending = %q, want 0", got)
	}
}

func TestRedisGuardReserveNormalSendRejectsFrozenAndMissingActive(t *testing.T) {
	guard, _, closeClient := newTestRedisGuard(t)
	defer closeClient()

	reservation, err := guard.ReserveNormalSend(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("ReserveNormalSend() error = %v", err)
	}
	if reservation.Kind != ReservationReject || reservation.Reason != ReasonTime {
		t.Fatalf("ReserveNormalSend() reservation = %#v, want reject time", reservation)
	}

	reservation, err = guard.ReserveNormalSend(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("ReserveNormalSend() frozen error = %v", err)
	}
	if reservation.Kind != ReservationReject || reservation.Reason != ReasonTime {
		t.Fatalf("ReserveNormalSend() frozen reservation = %#v, want reject time", reservation)
	}
}

func TestRedisGuardReserveNormalSendConcurrentThreshold(t *testing.T) {
	guard, redisServer, closeClient := newTestRedisGuard(t)
	defer closeClient()

	if err := guard.RecordActiveConversation(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RecordActiveConversation() error = %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := guard.ReserveNormalSend(context.Background(), "bot-1"); err != nil {
			t.Fatalf("ReserveNormalSend(%d) error = %v", i+1, err)
		}
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan Reservation, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			reservation, err := guard.ReserveNormalSend(context.Background(), "bot-1")
			if err != nil {
				errs <- err
				return
			}
			results <- reservation
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("ReserveNormalSend() concurrent error = %v", err)
	}

	counts := map[ReservationKind]int{}
	for reservation := range results {
		counts[reservation.Kind]++
	}
	if counts[ReservationSendNormalThenReminder] != 1 || counts[ReservationReject] != 1 {
		t.Fatalf("concurrent reservations = %#v, want one send_then_reminder and one reject", counts)
	}
	stateKey := guard.StateKey("bot-1")
	if got := redisServer.HGet(stateKey, "out_count"); got != "9" {
		t.Fatalf("out_count = %q, want 9", got)
	}
}

func TestRedisGuardReleaseNormalSendRestoresCriticalReservation(t *testing.T) {
	guard, redisServer, closeClient := newTestRedisGuard(t)
	defer closeClient()

	if err := guard.RecordActiveConversation(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RecordActiveConversation() error = %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := guard.ReserveNormalSend(context.Background(), "bot-1"); err != nil {
			t.Fatalf("ReserveNormalSend(%d) error = %v", i+1, err)
		}
	}
	if reservation, err := guard.ReserveNormalSend(context.Background(), "bot-1"); err != nil {
		t.Fatalf("ReserveNormalSend(critical) error = %v", err)
	} else if reservation.Kind != ReservationSendNormalThenReminder {
		t.Fatalf("ReserveNormalSend(critical) = %#v, want send normal then reminder", reservation)
	}

	if err := guard.ReleaseNormalSend(context.Background(), "bot-1"); err != nil {
		t.Fatalf("ReleaseNormalSend() error = %v", err)
	}

	stateKey := guard.StateKey("bot-1")
	if got := redisServer.HGet(stateKey, "out_count"); got != "8" {
		t.Fatalf("out_count = %q, want 8", got)
	}
	if got := redisServer.HGet(stateKey, "frozen"); got != "0" {
		t.Fatalf("frozen = %q, want 0", got)
	}

	reservation, err := guard.ReserveNormalSend(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("ReserveNormalSend(after release) error = %v", err)
	}
	if reservation.Kind != ReservationSendNormalThenReminder || reservation.Reason != ReasonCount {
		t.Fatalf("ReserveNormalSend(after release) = %#v, want count reminder", reservation)
	}
}

func TestRedisGuardReserveNormalSendTimeWarningSendsReminderOnly(t *testing.T) {
	guard, redisServer, closeClient := newTestRedisGuard(t)
	defer closeClient()

	if err := guard.RecordActiveConversation(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RecordActiveConversation() error = %v", err)
	}
	guard.client.Do(context.Background(), "PEXPIRE", guard.ActiveKey("bot-1"), int64((29 * time.Minute).Milliseconds()))

	reservation, err := guard.ReserveNormalSend(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("ReserveNormalSend() error = %v", err)
	}
	if reservation.Kind != ReservationSendReminderOnly || reservation.Reason != ReasonTime {
		t.Fatalf("ReserveNormalSend() reservation = %#v, want reminder only time", reservation)
	}
	stateKey := guard.StateKey("bot-1")
	if got := redisServer.HGet(stateKey, "out_count"); got != "0" {
		t.Fatalf("out_count = %q, want 0", got)
	}
	if got := redisServer.HGet(stateKey, "frozen"); got != "1" {
		t.Fatalf("frozen = %q, want 1", got)
	}
}

func TestRedisGuardCheckTimeWindowSendsSingleReminder(t *testing.T) {
	guard, _, closeClient := newTestRedisGuard(t)
	defer closeClient()

	if err := guard.RecordActiveConversation(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RecordActiveConversation() error = %v", err)
	}
	guard.client.Do(context.Background(), "PEXPIRE", guard.ActiveKey("bot-1"), int64((29 * time.Minute).Milliseconds()))

	decision, err := guard.CheckTimeWindow(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("CheckTimeWindow() error = %v", err)
	}
	if decision.Kind != DecisionSendReminderAndFreeze || decision.Reason != ReasonTime {
		t.Fatalf("CheckTimeWindow() decision = %#v, want reminder time", decision)
	}

	decision, err = guard.CheckTimeWindow(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("CheckTimeWindow() second error = %v", err)
	}
	if decision.Kind != DecisionReject || decision.Reason != ReasonTime {
		t.Fatalf("CheckTimeWindow() second decision = %#v, want reject time", decision)
	}
}

func TestRedisGuardBotStateIsIsolated(t *testing.T) {
	guard, _, closeClient := newTestRedisGuard(t)
	defer closeClient()

	for _, botID := range []string{"bot-A", "bot-B"} {
		if err := guard.RecordActiveConversation(context.Background(), botID); err != nil {
			t.Fatalf("RecordActiveConversation(%s) error = %v", botID, err)
		}
	}
	for i := 0; i < 9; i++ {
		if _, err := guard.ReserveNormalSend(context.Background(), "bot-A"); err != nil {
			t.Fatalf("ReserveNormalSend(bot-A, %d) error = %v", i+1, err)
		}
	}
	for i := 0; i < 2; i++ {
		reservation, err := guard.ReserveNormalSend(context.Background(), "bot-B")
		if err != nil {
			t.Fatalf("ReserveNormalSend(bot-B, %d) error = %v", i+1, err)
		}
		if reservation.Kind != ReservationSendNormal {
			t.Fatalf("ReserveNormalSend(bot-B, %d) reservation = %#v, want send normal", i+1, reservation)
		}
	}

	reservation, err := guard.ReserveNormalSend(context.Background(), "bot-B")
	if err != nil {
		t.Fatalf("ReserveNormalSend(bot-B) error = %v", err)
	}
	if reservation.Kind != ReservationSendNormal {
		t.Fatalf("ReserveNormalSend(bot-B) reservation = %#v, want send normal", reservation)
	}
}

func TestRedisGuardFailsClosedWhenRedisUnavailable(t *testing.T) {
	guard, _, closeClient := newTestRedisGuard(t)
	closeClient()

	_, err := guard.ReserveNormalSend(context.Background(), "bot-1")
	if err == nil {
		t.Fatal("ReserveNormalSend() error = nil, want redis rejection")
	}
	if !IsRejection(err) || RejectionReason(err) != "redis" {
		t.Fatalf("ReserveNormalSend() error = %v, want redis rejection", err)
	}
}

func newTestRedisGuard(t *testing.T) (*RedisGuard, *miniredis.Miniredis, func()) {
	t.Helper()

	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	guard := NewRedisGuard(client, RedisGuardConfig{
		KeyPrefix:               "webot-msg",
		MessageLimit:            10,
		MessageWarningRemaining: 1,
		ActiveWindow:            24 * time.Hour,
		TimeWarningBefore:       30 * time.Minute,
	})
	return guard, redisServer, func() {
		client.Close()
		redisServer.Close()
	}
}
