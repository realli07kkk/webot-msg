package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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
	messages []string
}

func (f *fakeMessageClient) SendMessage(_ config.UserConfig, _ string, text string, _ string) error {
	f.messages = append(f.messages, text)
	return nil
}

func (f *fakeMessageClient) SendTyping(config.UserConfig, int) error {
	return nil
}

type fakeProtectionGuard struct {
	reservation         protection.Reservation
	recordReminderCalls int
	releaseCalls        int
}

func (f *fakeProtectionGuard) ReserveNormalSend(context.Context, string) (protection.Reservation, error) {
	if f.reservation.Kind != protection.ReservationSendNormal {
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
