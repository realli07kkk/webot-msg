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
	"github.com/realli07kkk/webot-msg/internal/sender"
)

const fixedMessageID = "01890f3e-6f44-7b2c-8d9e-123456789abc"

func TestHandleSendMessageSendsReminderAfterNormalSendDecision(t *testing.T) {
	store := newAPIStore(t)
	client := &fakeMessageClient{}
	guard := &fakeProtectionGuard{
		reservation: protection.SendNormalThenReminder(protection.ReasonCount),
	}
	server := newTestServer(store, client, guard)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bots/bot-1/messages?token=api-token&text=hello", nil)
	server.handleBotAction(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
	wantNormal := "hello\n" + fixedMessageID
	if got := strings.Join(client.messages, ","); got != wantNormal+",reminder" {
		t.Fatalf("messages = %q, want %q,reminder", got, wantNormal)
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
	server := newTestServer(store, client, guard)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bots/bot-1/messages?token=api-token&text=hello", nil)
	server.handleBotAction(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
	wantMessage := "hello\n[限流阈值] 剩余可发 4 条 | 距离限制还有 9h30m\n" + fixedMessageID
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
	server := newTestServer(store, client, guard)

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

func TestHandleSendMessageWithQueueControllerSendsNow(t *testing.T) {
	store := newAPIStore(t)
	client := &fakeMessageClient{}
	guard := &fakeQueueProtectionGuard{
		ingress: protection.Ingress{
			Outcome:     protection.IngressSendNow,
			Reservation: protection.SendNormal(),
		},
	}
	server := newTestServer(store, client, guard)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bots/bot-1/messages?token=api-token&text=hello", nil)
	server.handleBotAction(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
	want := "hello\n" + fixedMessageID
	if got := client.messages; len(got) != 1 || got[0] != want {
		t.Fatalf("messages = %#v, want [%q]", got, want)
	}
	if guard.acquireCalls != 1 {
		t.Fatalf("AcquireOrEnqueue calls = %d, want 1", guard.acquireCalls)
	}
}

func TestHandleSendMessageQueuesFrozenWithoutSending(t *testing.T) {
	store := newAPIStore(t)
	client := &fakeMessageClient{}
	guard := &fakeQueueProtectionGuard{
		ingress: protection.Ingress{
			Outcome:  protection.IngressQueued,
			QueueLen: 3,
			Reason:   protection.ReasonCount,
		},
	}
	server := newTestServer(store, client, guard)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bots/bot-1/messages?token=api-token&text=hello", nil)
	server.handleBotAction(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s, want 202", rr.Code, rr.Body.String())
	}
	if len(client.messages) != 0 {
		t.Fatalf("messages = %#v, want none", client.messages)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("response JSON decode error = %v", err)
	}
	if body["status"] != "queued" || body["queued"] != float64(3) {
		t.Fatalf("response body = %#v, want queued length 3", body)
	}
}

func TestHandleSendMessageReturnsServiceUnavailableWhenQueueFull(t *testing.T) {
	store := newAPIStore(t)
	client := &fakeMessageClient{}
	guard := &fakeQueueProtectionGuard{
		ingress: protection.Ingress{
			Outcome: protection.IngressQueueFull,
			Reason:  protection.ReasonCount,
		},
	}
	server := newTestServer(store, client, guard)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bots/bot-1/messages?token=api-token&text=hello", nil)
	server.handleBotAction(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s, want 503", rr.Code, rr.Body.String())
	}
	if len(client.messages) != 0 {
		t.Fatalf("messages = %#v, want none", client.messages)
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

func newTestServer(store *config.Store, client messageClient, guard protection.Guard) *Server {
	return NewServerWithClientOptions(store, client, guard, "reminder", sender.TextOptions{
		IDGenerator: func() (string, error) {
			return fixedMessageID, nil
		},
	})
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

type fakeQueueProtectionGuard struct {
	fakeProtectionGuard
	ingress      protection.Ingress
	acquireCalls int
}

func (f *fakeQueueProtectionGuard) AcquireOrEnqueue(context.Context, string, string) (protection.Ingress, error) {
	f.acquireCalls++
	return f.ingress, nil
}

func (f *fakeQueueProtectionGuard) PeekQueued(context.Context, string) (string, int64, bool, error) {
	return "", 0, false, nil
}

func (f *fakeQueueProtectionGuard) DropFront(context.Context, string) error {
	return nil
}

func (f *fakeQueueProtectionGuard) QueueLen(context.Context, string) (int, error) {
	return 0, nil
}
