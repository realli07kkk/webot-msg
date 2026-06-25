package protection

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestRuntimeGuardDefaultsToDisabledNoop(t *testing.T) {
	guard := NewRuntimeGuard()

	if guard.Enabled() {
		t.Fatal("Enabled() = true, want false")
	}
	reservation, err := guard.ReserveNormalSend(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("ReserveNormalSend() error = %v", err)
	}
	if reservation.Kind != ReservationSendNormal {
		t.Fatalf("ReserveNormalSend() = %#v, want send normal", reservation)
	}
	decision, err := guard.CheckTimeWindow(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("CheckTimeWindow() error = %v", err)
	}
	if decision.Kind != DecisionAllow {
		t.Fatalf("CheckTimeWindow() = %#v, want allow", decision)
	}
}

func TestRuntimeGuardEnableAndDisable(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	defer redisServer.Close()

	guard := NewRuntimeGuard()
	err = guard.Enable(context.Background(), EnableConfig{
		RedisURL:                "redis://" + redisServer.Addr() + "/0",
		KeyPrefix:               "webot-msg",
		MessageLimit:            10,
		MessageWarningRemaining: 1,
		ActiveWindow:            24 * time.Hour,
		TimeWarningBefore:       30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if !guard.Enabled() {
		t.Fatal("Enabled() = false, want true")
	}

	reservation, err := guard.ReserveNormalSend(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("ReserveNormalSend() error = %v", err)
	}
	if reservation.Kind != ReservationReject || reservation.Reason != ReasonTime {
		t.Fatalf("ReserveNormalSend() = %#v, want Redis time rejection", reservation)
	}

	guard.Disable()
	if guard.Enabled() {
		t.Fatal("Enabled() = true, want false")
	}
	reservation, err = guard.ReserveNormalSend(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("ReserveNormalSend() after Disable error = %v", err)
	}
	if reservation.Kind != ReservationSendNormal {
		t.Fatalf("ReserveNormalSend() after Disable = %#v, want send normal", reservation)
	}
}

func TestRuntimeGuardOperationKeepsGenerationAfterDisable(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	defer redisServer.Close()

	guard := NewRuntimeGuard()
	err = guard.Enable(context.Background(), EnableConfig{
		RedisURL:                "redis://" + redisServer.Addr() + "/0",
		KeyPrefix:               "webot-msg",
		MessageLimit:            10,
		MessageWarningRemaining: 1,
		ActiveWindow:            24 * time.Hour,
		TimeWarningBefore:       30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if err := guard.RecordActiveConversation(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RecordActiveConversation() error = %v", err)
	}

	operation := guard.BeginOperation()
	reservation, err := operation.ReserveNormalSend(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("ReserveNormalSend() error = %v", err)
	}
	if reservation.Kind != ReservationSendNormal {
		t.Fatalf("ReserveNormalSend() = %#v, want send normal", reservation)
	}

	guard.Disable()
	if err := operation.ReleaseNormalSend(context.Background(), "bot-1"); err != nil {
		t.Fatalf("ReleaseNormalSend() after Disable error = %v", err)
	}
	operation.Done()

	stateKey := "webot-msg:protect:{bot-1}:state"
	if got := redisServer.HGet(stateKey, "out_count"); got != "0" {
		t.Fatalf("out_count = %q, want 0 after operation release", got)
	}
}

func TestRuntimeGuardEnableFailsWithoutChangingState(t *testing.T) {
	guard := NewRuntimeGuard()

	err := guard.Enable(context.Background(), EnableConfig{
		RedisURL:                "",
		KeyPrefix:               "webot-msg",
		MessageLimit:            10,
		MessageWarningRemaining: 1,
		ActiveWindow:            24 * time.Hour,
		TimeWarningBefore:       30 * time.Minute,
	})
	if err == nil {
		t.Fatal("Enable() error = nil, want error")
	}
	if guard.Enabled() {
		t.Fatal("Enabled() = true, want false")
	}
}

func TestRuntimeGuardStatusWhenDisabled(t *testing.T) {
	guard := NewRuntimeGuard()

	status, err := guard.RuntimeStatus(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("RuntimeStatus() error = %v", err)
	}
	if status.Enabled {
		t.Fatal("Status.Enabled = true, want false")
	}
	if status.BotID != "bot-1" {
		t.Fatalf("Status.BotID = %q, want bot-1", status.BotID)
	}
}

func TestRuntimeGuardOperationExposesSendQueueController(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	defer redisServer.Close()

	guard := NewRuntimeGuard()
	if err := guard.Enable(context.Background(), EnableConfig{
		RedisURL:                "redis://" + redisServer.Addr() + "/0",
		KeyPrefix:               "webot-msg",
		MessageLimit:            10,
		MessageWarningRemaining: 1,
		ActiveWindow:            24 * time.Hour,
		TimeWarningBefore:       30 * time.Minute,
		QueueMaxLen:             2,
		QueueTTL:                24 * time.Hour,
	}); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}

	operation := guard.BeginOperation()
	defer operation.Done()
	controller, ok := operation.(SendQueueController)
	if !ok {
		t.Fatal("runtime operation does not expose SendQueueController")
	}
	ingress, err := controller.AcquireOrEnqueue(context.Background(), "bot-1", "queued")
	if err != nil {
		t.Fatalf("AcquireOrEnqueue() error = %v", err)
	}
	if ingress.Outcome != IngressQueued || ingress.QueueLen != 1 {
		t.Fatalf("AcquireOrEnqueue() = %#v, want queued length 1", ingress)
	}
	queueLen, err := controller.QueueLen(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("QueueLen() error = %v", err)
	}
	if queueLen != 1 {
		t.Fatalf("QueueLen() = %d, want 1", queueLen)
	}
}
