package sender

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/protection"
)

func TestSendProtectedTextFailsClosedWhenReminderRecordFails(t *testing.T) {
	client := &fakeMessageClient{}
	guard := &fakeGuard{
		reservation:       protection.SendNormalThenReminder(protection.ReasonCount),
		recordReminderErr: protection.NewRejection("redis", errors.New("redis down")),
	}
	user := config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}

	_, err := SendProtectedText(context.Background(), client, guard, user, "hello", "reminder")
	if err == nil {
		t.Fatal("SendProtectedText() error = nil, want reminder record failure")
	}
	if !protection.IsRejection(err) || protection.RejectionReason(err) != "redis" {
		t.Fatalf("SendProtectedText() error = %v, want redis rejection", err)
	}
	if got := len(client.messages); got != 2 {
		t.Fatalf("sent messages = %#v, want normal + reminder", client.messages)
	}
}

func TestSendProtectedTextAppendsStatusFooterWhenSnapshotExists(t *testing.T) {
	client := &fakeMessageClient{}
	guard := &fakeGuard{
		reservation: protection.Reservation{
			Kind:                   protection.ReservationSendNormal,
			HasStatus:              true,
			MessagesBeforeReminder: 4,
			TimeBeforeWarning:      9*time.Hour + 30*time.Minute,
		},
	}
	user := config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}

	result, err := SendProtectedText(context.Background(), client, guard, user, "hello", "reminder")
	if err != nil {
		t.Fatalf("SendProtectedText() error = %v", err)
	}
	if !result.NormalSent || result.ReminderSent {
		t.Fatalf("SendProtectedText() = %#v, want normal only", result)
	}
	want := "hello\n[限流阈值] 剩余可发 4 条 | 距离限制还有 9h30m"
	if got := client.messages; len(got) != 1 || got[0] != want {
		t.Fatalf("messages = %#v, want [%q]", got, want)
	}
}

func TestSendProtectedTextLeavesTextUnchangedWithoutStatusSnapshot(t *testing.T) {
	client := &fakeMessageClient{}
	user := config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}

	result, err := SendProtectedText(context.Background(), client, protection.NoopGuard{}, user, "hello", "reminder")
	if err != nil {
		t.Fatalf("SendProtectedText() error = %v", err)
	}
	if !result.NormalSent || result.ReminderSent {
		t.Fatalf("SendProtectedText() = %#v, want normal only", result)
	}
	if got := client.messages; len(got) != 1 || got[0] != "hello" {
		t.Fatalf("messages = %#v, want [hello]", got)
	}
}

func TestSendProtectedTextDoesNotAppendStatusFooterToReminder(t *testing.T) {
	client := &fakeMessageClient{}
	guard := &fakeGuard{
		reservation: protection.Reservation{
			Kind:                   protection.ReservationSendNormalThenReminder,
			Reason:                 protection.ReasonCount,
			HasStatus:              true,
			MessagesBeforeReminder: 0,
			TimeBeforeWarning:      9*time.Hour + 25*time.Minute,
		},
	}
	user := config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}

	result, err := SendProtectedText(context.Background(), client, guard, user, "hello", "reminder")
	if err != nil {
		t.Fatalf("SendProtectedText() error = %v", err)
	}
	if !result.NormalSent || !result.ReminderSent {
		t.Fatalf("SendProtectedText() = %#v, want normal and reminder sent", result)
	}
	wantNormal := "hello\n[限流阈值] 剩余可发 0 条 | 距离限制还有 9h25m"
	if got := client.messages; len(got) != 2 || got[0] != wantNormal || got[1] != "reminder" {
		t.Fatalf("messages = %#v, want [%q reminder]", got, wantNormal)
	}
}

func TestSendProtectedTextReleaseUsesReservedGenerationAfterDisable(t *testing.T) {
	guard, redisServer := newRuntimeGuardWithRedis(t)
	seedProtectionCount(t, guard, "bot-1", 8)

	client := &fakeMessageClient{
		sendErr: errors.New("remote down"),
		afterSend: func(string) {
			guard.Disable()
		},
	}
	user := config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}

	_, err := SendProtectedText(context.Background(), client, guard, user, "hello", "reminder")
	if err == nil {
		t.Fatal("SendProtectedText() error = nil, want send error")
	}

	stateKey := "webot-msg:protect:{bot-1}:state"
	if got := redisServer.HGet(stateKey, "out_count"); got != "8" {
		t.Fatalf("out_count = %q, want 8 after release", got)
	}
	if got := redisServer.HGet(stateKey, "frozen"); got != "0" {
		t.Fatalf("frozen = %q, want 0 after release", got)
	}
	if got := redisServer.HGet(stateKey, "reminder_pending"); got != "0" {
		t.Fatalf("reminder_pending = %q, want 0 after release", got)
	}
}

func TestSendProtectedTextReminderRecordUsesReservedGenerationAfterDisable(t *testing.T) {
	guard, redisServer := newRuntimeGuardWithRedis(t)
	seedProtectionCount(t, guard, "bot-1", 8)

	client := &fakeMessageClient{
		afterSend: func(text string) {
			if text == "reminder" {
				guard.Disable()
			}
		},
	}
	user := config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}

	result, err := SendProtectedText(context.Background(), client, guard, user, "hello", "reminder")
	if err != nil {
		t.Fatalf("SendProtectedText() error = %v", err)
	}
	if !result.NormalSent || !result.ReminderSent {
		t.Fatalf("SendProtectedText() = %#v, want normal and reminder sent", result)
	}
	wantNormal := "hello\n[限流阈值] 剩余可发 0 条 | 距离限制还有 23h30m"
	if got := client.messages; len(got) != 2 || got[0] != wantNormal || got[1] != "reminder" {
		t.Fatalf("messages = %#v, want [%q reminder]", got, wantNormal)
	}

	stateKey := "webot-msg:protect:{bot-1}:state"
	if got := redisServer.HGet(stateKey, "out_count"); got != "10" {
		t.Fatalf("out_count = %q, want 10 after reminder record", got)
	}
	if got := redisServer.HGet(stateKey, "frozen"); got != "1" {
		t.Fatalf("frozen = %q, want 1 after reminder record", got)
	}
	if got := redisServer.HGet(stateKey, "reminder_pending"); got != "0" {
		t.Fatalf("reminder_pending = %q, want 0 after reminder record", got)
	}
}

func newRuntimeGuardWithRedis(t *testing.T) (*protection.RuntimeGuard, *miniredis.Miniredis) {
	t.Helper()

	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	t.Cleanup(redisServer.Close)

	guard := protection.NewRuntimeGuard()
	if err := guard.Enable(context.Background(), protection.EnableConfig{
		RedisURL:                "redis://" + redisServer.Addr() + "/0",
		KeyPrefix:               "webot-msg",
		MessageLimit:            10,
		MessageWarningRemaining: 1,
		ActiveWindow:            24 * time.Hour,
		TimeWarningBefore:       30 * time.Minute,
	}); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	return guard, redisServer
}

func seedProtectionCount(t *testing.T, guard protection.Guard, botID string, count int) {
	t.Helper()

	if err := guard.RecordActiveConversation(context.Background(), botID); err != nil {
		t.Fatalf("RecordActiveConversation() error = %v", err)
	}
	for i := 0; i < count; i++ {
		reservation, err := guard.ReserveNormalSend(context.Background(), botID)
		if err != nil {
			t.Fatalf("ReserveNormalSend(%d) error = %v", i, err)
		}
		if reservation.Kind != protection.ReservationSendNormal {
			t.Fatalf("ReserveNormalSend(%d) = %#v, want send normal", i, reservation)
		}
	}
}

type fakeMessageClient struct {
	messages  []string
	sendErr   error
	afterSend func(text string)
}

func (f *fakeMessageClient) SendMessage(_ config.UserConfig, _ string, text string, _ string) error {
	f.messages = append(f.messages, text)
	if f.afterSend != nil {
		f.afterSend(text)
	}
	return f.sendErr
}

type fakeGuard struct {
	reservation       protection.Reservation
	recordReminderErr error
}

func (f *fakeGuard) ReserveNormalSend(context.Context, string) (protection.Reservation, error) {
	if f.reservation.Kind != protection.ReservationSendNormal || f.reservation.Reason != "" || f.reservation.HasStatus {
		return f.reservation, nil
	}
	return protection.SendNormal(), nil
}

func (f *fakeGuard) ReleaseNormalSend(context.Context, string) error {
	return nil
}

func (f *fakeGuard) RecordReminderSend(context.Context, string) error {
	return f.recordReminderErr
}

func (f *fakeGuard) RecordActiveConversation(context.Context, string) error {
	return nil
}

func (f *fakeGuard) CheckTimeWindow(context.Context, string) (protection.Decision, error) {
	return protection.Allow(), nil
}
