package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/protection"
)

func TestHandleSendMessageSendsReminderAfterNormalSendDecision(t *testing.T) {
	store := newAPIStore(t)
	client := &fakeMessageClient{}
	guard := &fakeProtectionGuard{
		reservation: protection.SendNormalThenReminder(protection.ReasonCount),
	}
	server := NewServerWithClient(store, client, guard, "reminder")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bots/bot-1/messages?token=api-token&text=hello", nil)
	server.handleBotAction(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
	if got := strings.Join(client.messages, ","); got != "hello,reminder" {
		t.Fatalf("messages = %q, want hello,reminder", got)
	}
	if guard.recordReminderCalls != 1 {
		t.Fatalf("RecordReminderSend calls = %d, want 1", guard.recordReminderCalls)
	}
}

func TestHandleSendMessageAppendsStatusFooterWithoutChangingResponse(t *testing.T) {
	store := newAPIStore(t)
	client := &fakeMessageClient{}
	guard := &fakeProtectionGuard{
		reservation: protection.Reservation{
			Kind:                   protection.ReservationSendNormal,
			HasStatus:              true,
			MessagesBeforeReminder: 4,
			TimeBeforeWarning:      9*time.Hour + 30*time.Minute,
		},
	}
	server := NewServerWithClient(store, client, guard, "reminder")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bots/bot-1/messages?token=api-token&text=hello", nil)
	server.handleBotAction(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
	wantMessage := "hello\n[限流阈值] 剩余可发 4 条 | 距离限制还有 9h30m"
	if got := client.messages; len(got) != 1 || got[0] != wantMessage {
		t.Fatalf("messages = %#v, want [%q]", got, wantMessage)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("response JSON decode error = %v", err)
	}
	if _, ok := body["text"]; ok {
		t.Fatalf("response body = %#v, must not include text", body)
	}
	if _, ok := body["message_text"]; ok {
		t.Fatalf("response body = %#v, must not include message_text", body)
	}
}

func TestHandleSendMessageRejectsFrozenBeforeSendingUserText(t *testing.T) {
	store := newAPIStore(t)
	client := &fakeMessageClient{}
	guard := &fakeProtectionGuard{
		reservation: protection.RejectNormal(protection.ReasonCount),
	}
	server := NewServerWithClient(store, client, guard, "reminder")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bots/bot-1/messages?token=api-token&text=hello", nil)
	server.handleBotAction(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s, want 429", rr.Code, rr.Body.String())
	}
	if len(client.messages) != 0 {
		t.Fatalf("messages = %#v, want none", client.messages)
	}
	if !strings.Contains(rr.Body.String(), "protection mode locked") {
		t.Fatalf("body = %q, want protection lock message", rr.Body.String())
	}
}

func TestHandleTypingDoesNotReserveOrAppendStatusFooter(t *testing.T) {
	store := newAPIStore(t)
	client := &fakeMessageClient{}
	guard := &fakeProtectionGuard{
		reservation: protection.Reservation{
			Kind:                   protection.ReservationSendNormal,
			HasStatus:              true,
			MessagesBeforeReminder: 4,
			TimeBeforeWarning:      9*time.Hour + 30*time.Minute,
		},
	}
	server := NewServerWithClient(store, client, guard, "reminder")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bots/bot-1/typing?token=api-token&status=1", nil)
	server.handleBotAction(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
	if guard.reserveCalls != 0 {
		t.Fatalf("ReserveNormalSend calls = %d, want 0", guard.reserveCalls)
	}
	if len(client.messages) != 0 {
		t.Fatalf("messages = %#v, want none", client.messages)
	}
	if got := client.typingStatuses; len(got) != 1 || got[0] != 1 {
		t.Fatalf("typing statuses = %#v, want [1]", got)
	}
}

func newAPIStore(t *testing.T) *config.Store {
	t.Helper()
	store := config.NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if err := store.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}
	if err := store.AddBot(config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
		APIToken:     "api-token",
	}); err != nil {
		t.Fatalf("AddBot() error = %v", err)
	}
	return store
}

type fakeMessageClient struct {
	messages       []string
	typingStatuses []int
}

func (f *fakeMessageClient) SendMessageContext(_ context.Context, _ config.UserConfig, _ string, text string, _ string) error {
	f.messages = append(f.messages, text)
	return nil
}

func (f *fakeMessageClient) SendTypingContext(_ context.Context, _ config.UserConfig, status int) error {
	f.typingStatuses = append(f.typingStatuses, status)
	return nil
}

type fakeProtectionGuard struct {
	reservation         protection.Reservation
	reserveCalls        int
	recordReminderCalls int
	releaseCalls        int
}

func (f *fakeProtectionGuard) ReserveNormalSend(context.Context, string) (protection.Reservation, error) {
	f.reserveCalls++
	if f.reservation.Kind != protection.ReservationSendNormal || f.reservation.Reason != "" || f.reservation.HasStatus {
		return f.reservation, nil
	}
	return protection.SendNormal(), nil
}

func (f *fakeProtectionGuard) ReleaseNormalSend(context.Context, string) error {
	f.releaseCalls++
	return nil
}

func (f *fakeProtectionGuard) RecordReminderSend(context.Context, string) error {
	f.recordReminderCalls++
	return nil
}

func (f *fakeProtectionGuard) RecordActiveConversation(context.Context, string) error {
	return nil
}

func (f *fakeProtectionGuard) CheckTimeWindow(context.Context, string) (protection.Decision, error) {
	return protection.Allow(), nil
}
