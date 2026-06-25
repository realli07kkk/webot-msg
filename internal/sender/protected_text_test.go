package sender

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/realli07kkk/webot-msg/internal/audit"
	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/protection"
)

const fixedMessageID = "01890f3e-6f44-7b2c-8d9e-123456789abc"

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

	_, err := sendProtectedTextForTest(context.Background(), client, guard, user, "hello", "reminder", TextOptions{})
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
	auditor := &fakeAuditor{}
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

	result, err := sendProtectedTextForTest(context.Background(), client, guard, user, "hello", "reminder", TextOptions{
		Auditor: auditor,
		Now:     fixedNow,
	})
	if err != nil {
		t.Fatalf("SendProtectedText() error = %v", err)
	}
	if !result.NormalSent || result.ReminderSent {
		t.Fatalf("SendProtectedText() = %#v, want normal only", result)
	}
	want := "hello\n[限流阈值] 剩余可发 4 条 | 距离限制还有 9h30m\n" + fixedMessageID
	if got := client.messages; len(got) != 1 || got[0] != want {
		t.Fatalf("messages = %#v, want [%q]", got, want)
	}
	if len(auditor.records) != 1 {
		t.Fatalf("audit records = %#v, want one", auditor.records)
	}
	if got := auditor.records[0]; got.ID != fixedMessageID || got.Body != want || !got.SentAt.Equal(fixedNow()) {
		t.Fatalf("audit record = %#v, want id/body/sentAt for final text", got)
	}
}

func TestSendProtectedTextAppendsIDWithoutStatusSnapshot(t *testing.T) {
	client := &fakeMessageClient{}
	user := config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}

	result, err := sendProtectedTextForTest(context.Background(), client, protection.NoopGuard{}, user, "hello", "reminder", TextOptions{})
	if err != nil {
		t.Fatalf("SendProtectedText() error = %v", err)
	}
	if !result.NormalSent || result.ReminderSent {
		t.Fatalf("SendProtectedText() = %#v, want normal only", result)
	}
	want := "hello\n" + fixedMessageID
	if got := client.messages; len(got) != 1 || got[0] != want {
		t.Fatalf("messages = %#v, want [%q]", got, want)
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

	result, err := sendProtectedTextForTest(context.Background(), client, guard, user, "hello", "reminder", TextOptions{})
	if err != nil {
		t.Fatalf("SendProtectedText() error = %v", err)
	}
	if !result.NormalSent || !result.ReminderSent {
		t.Fatalf("SendProtectedText() = %#v, want normal and reminder sent", result)
	}
	wantNormal := "hello\n[限流阈值] 剩余可发 0 条 | 距离限制还有 9h25m\n" + fixedMessageID
	if got := client.messages; len(got) != 2 || got[0] != wantNormal || got[1] != "reminder" {
		t.Fatalf("messages = %#v, want [%q reminder]", got, wantNormal)
	}
}

func TestSendProtectedTextDefaultIDGeneratorAppendsUUIDV7(t *testing.T) {
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
	if got := client.messages; len(got) != 1 {
		t.Fatalf("messages = %#v, want one message", got)
	}
	lines := strings.Split(client.messages[0], "\n")
	if len(lines) != 2 || lines[0] != "hello" || len(lines[1]) != len(fixedMessageID) || lines[1][14] != '7' {
		t.Fatalf("message = %q, want hello plus uuid v7 line", client.messages[0])
	}
}

func TestSendProtectedTextIDGenerationFailureSkipsAuditAndStillSends(t *testing.T) {
	client := &fakeMessageClient{}
	auditor := &fakeAuditor{}
	user := config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}

	result, err := SendProtectedTextWithOptions(context.Background(), client, protection.NoopGuard{}, user, "hello", "reminder", TextOptions{
		IDGenerator: func() (string, error) {
			return "", errors.New("random failed")
		},
		Auditor: auditor,
	})
	if err != nil {
		t.Fatalf("SendProtectedText() error = %v", err)
	}
	if !result.NormalSent || result.ReminderSent {
		t.Fatalf("SendProtectedText() = %#v, want normal only", result)
	}
	if got := client.messages; len(got) != 1 || got[0] != "hello" {
		t.Fatalf("messages = %#v, want [hello]", got)
	}
	if len(auditor.records) != 0 {
		t.Fatalf("audit records = %#v, want none", auditor.records)
	}
}

func TestSendProtectedTextAuditFailureDoesNotFailSend(t *testing.T) {
	client := &fakeMessageClient{}
	auditor := &fakeAuditor{err: errors.New("redis down")}
	user := config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}

	result, err := sendProtectedTextForTest(context.Background(), client, protection.NoopGuard{}, user, "hello", "reminder", TextOptions{
		Auditor: auditor,
	})
	if err != nil {
		t.Fatalf("SendProtectedText() error = %v, want nil on audit failure", err)
	}
	if !result.NormalSent || result.ReminderSent {
		t.Fatalf("SendProtectedText() = %#v, want normal only", result)
	}
	want := "hello\n" + fixedMessageID
	if got := client.messages; len(got) != 1 || got[0] != want {
		t.Fatalf("messages = %#v, want [%q]", got, want)
	}
}

func TestSendOrEnqueueTextFallsBackToProtectedSend(t *testing.T) {
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

	outcome, err := SendOrEnqueueText(context.Background(), client, guard, user, "hello", "reminder", TextOptions{
		IDGenerator: func() (string, error) {
			return fixedMessageID, nil
		},
	})
	if err != nil {
		t.Fatalf("SendOrEnqueueText() error = %v", err)
	}
	if outcome.Kind != OutcomeSent || !outcome.Result.NormalSent || outcome.Result.ReminderSent {
		t.Fatalf("SendOrEnqueueText() = %#v, want sent normal only", outcome)
	}
	if guard.reserveCalls != 1 {
		t.Fatalf("ReserveNormalSend calls = %d, want 1", guard.reserveCalls)
	}
	want := "hello\n[限流阈值] 剩余可发 4 条 | 距离限制还有 9h30m\n" + fixedMessageID
	if got := client.messages; len(got) != 1 || got[0] != want {
		t.Fatalf("messages = %#v, want [%q]", got, want)
	}
}

func TestSendOrEnqueueTextQueuesWithoutSending(t *testing.T) {
	client := &fakeMessageClient{}
	guard := &fakeQueueGuard{
		ingress: protection.Ingress{
			Outcome:  protection.IngressQueued,
			QueueLen: 1,
			Reason:   protection.ReasonCount,
		},
	}
	user := config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}

	outcome, err := SendOrEnqueueText(context.Background(), client, guard, user, "hello", "reminder", TextOptions{})
	if err != nil {
		t.Fatalf("SendOrEnqueueText() error = %v", err)
	}
	if outcome.Kind != OutcomeQueued || outcome.QueueLen != 1 {
		t.Fatalf("SendOrEnqueueText() = %#v, want queued length 1", outcome)
	}
	if guard.acquireCalls != 1 {
		t.Fatalf("AcquireOrEnqueue calls = %d, want 1", guard.acquireCalls)
	}
	if guard.reserveCalls != 0 {
		t.Fatalf("ReserveNormalSend calls = %d, want 0 on queued path", guard.reserveCalls)
	}
	if len(client.messages) != 0 {
		t.Fatalf("messages = %#v, want none", client.messages)
	}
}

func TestSendOrEnqueueTextQueuedReminderSendsReminderOnly(t *testing.T) {
	client := &fakeMessageClient{}
	guard := &fakeQueueGuard{
		ingress: protection.Ingress{
			Outcome:      protection.IngressQueued,
			QueueLen:     1,
			SendReminder: true,
			Reason:       protection.ReasonTime,
		},
	}
	user := config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}

	outcome, err := SendOrEnqueueText(context.Background(), client, guard, user, "hello", "reminder", TextOptions{})
	if err != nil {
		t.Fatalf("SendOrEnqueueText() error = %v", err)
	}
	if outcome.Kind != OutcomeQueued || !outcome.Result.ReminderSent || outcome.Result.ReminderReason != protection.ReasonTime {
		t.Fatalf("SendOrEnqueueText() = %#v, want queued reminder", outcome)
	}
	if got := client.messages; len(got) != 1 || got[0] != "reminder" {
		t.Fatalf("messages = %#v, want reminder only", got)
	}
	if guard.recordReminderCalls != 1 {
		t.Fatalf("RecordReminderSend calls = %d, want 1", guard.recordReminderCalls)
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

	_, err := sendProtectedTextForTest(context.Background(), client, guard, user, "hello", "reminder", TextOptions{})
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

	result, err := sendProtectedTextForTest(context.Background(), client, guard, user, "hello", "reminder", TextOptions{})
	if err != nil {
		t.Fatalf("SendProtectedText() error = %v", err)
	}
	if !result.NormalSent || !result.ReminderSent {
		t.Fatalf("SendProtectedText() = %#v, want normal and reminder sent", result)
	}
	wantNormal := "hello\n[限流阈值] 剩余可发 0 条 | 距离限制还有 23h30m\n" + fixedMessageID
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

func sendProtectedTextForTest(ctx context.Context, client MessageClient, guard protection.Guard, user config.UserConfig, text string, reminderText string, opts TextOptions) (TextResult, error) {
	if opts.IDGenerator == nil {
		opts.IDGenerator = func() (string, error) {
			return fixedMessageID, nil
		}
	}
	return SendProtectedTextWithOptions(ctx, client, guard, user, text, reminderText, opts)
}

func fixedNow() time.Time {
	return time.Unix(1700000000, 123000000)
}

type fakeMessageClient struct {
	messages  []string
	sendErr   error
	afterSend func(text string)
}

func (f *fakeMessageClient) SendMessageContext(_ context.Context, _ config.UserConfig, _ string, text string, _ string) error {
	f.messages = append(f.messages, text)
	if f.afterSend != nil {
		f.afterSend(text)
	}
	return f.sendErr
}

type fakeGuard struct {
	reservation         protection.Reservation
	recordReminderErr   error
	reserveCalls        int
	recordReminderCalls int
}

func (f *fakeGuard) ReserveNormalSend(context.Context, string) (protection.Reservation, error) {
	f.reserveCalls++
	if f.reservation.Kind != protection.ReservationSendNormal || f.reservation.Reason != "" || f.reservation.HasStatus {
		return f.reservation, nil
	}
	return protection.SendNormal(), nil
}

func (f *fakeGuard) ReleaseNormalSend(context.Context, string) error {
	return nil
}

func (f *fakeGuard) RecordReminderSend(context.Context, string) error {
	f.recordReminderCalls++
	return f.recordReminderErr
}

func (f *fakeGuard) RecordActiveConversation(context.Context, string) error {
	return nil
}

func (f *fakeGuard) CheckTimeWindow(context.Context, string) (protection.Decision, error) {
	return protection.Allow(), nil
}

type fakeQueueGuard struct {
	fakeGuard
	ingress      protection.Ingress
	acquireCalls int
}

func (f *fakeQueueGuard) AcquireOrEnqueue(context.Context, string, string) (protection.Ingress, error) {
	f.acquireCalls++
	return f.ingress, nil
}

func (f *fakeQueueGuard) PeekQueued(context.Context, string) (string, int64, bool, error) {
	return "", 0, false, nil
}

func (f *fakeQueueGuard) DropFront(context.Context, string) error {
	return nil
}

func (f *fakeQueueGuard) QueueLen(context.Context, string) (int, error) {
	return 0, nil
}

type fakeAuditor struct {
	records []audit.RecordInput
	err     error
}

func (f *fakeAuditor) Enabled() bool {
	return true
}

func (f *fakeAuditor) Record(_ context.Context, in audit.RecordInput) error {
	if f.err != nil {
		return f.err
	}
	f.records = append(f.records, in)
	return nil
}
