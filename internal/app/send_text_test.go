package app

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/ilink"
	"github.com/realli07kkk/webot-msg/internal/protection"
)

const fixedMessageID = "01890f3e-6f44-7b2c-8d9e-123456789abc"

func TestSendTextSendsReminderAfterNormalSendDecision(t *testing.T) {
	store := config.NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if err := store.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}
	if err := store.AddBot(config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}); err != nil {
		t.Fatalf("AddBot() error = %v", err)
	}

	client := &fakeClient{}
	guard := &fakeGuard{
		reservation: protection.SendNormalThenReminder(protection.ReasonCount),
	}
	a := &App{
		store:        store,
		client:       client,
		guard:        guard,
		idGenerator:  fixedIDGenerator,
		reminderText: "reminder",
	}

	if err := a.SendText("bot-1", "hello"); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	if len(client.messages) != 2 {
		t.Fatalf("sent messages = %#v, want normal + reminder", client.messages)
	}
	wantNormal := "hello\n" + fixedMessageID
	if client.messages[0] != wantNormal || client.messages[1] != "reminder" {
		t.Fatalf("sent messages = %#v, want [%q reminder]", client.messages, wantNormal)
	}
	if guard.recordReminderCalls != 1 {
		t.Fatalf("RecordReminderSend calls = %d, want 1", guard.recordReminderCalls)
	}
}

func TestSendTextAppendsStatusFooter(t *testing.T) {
	store := config.NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if err := store.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}
	if err := store.AddBot(config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}); err != nil {
		t.Fatalf("AddBot() error = %v", err)
	}

	client := &fakeClient{}
	a := &App{
		store:       store,
		client:      client,
		idGenerator: fixedIDGenerator,
		guard: &fakeGuard{
			reservation: protection.Reservation{
				Kind:                   protection.ReservationSendNormal,
				HasStatus:              true,
				MessagesBeforeReminder: 4,
				TimeBeforeWarning:      9*time.Hour + 30*time.Minute,
			},
		},
		reminderText: "reminder",
	}

	if err := a.SendText("bot-1", "hello"); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	want := "hello\n[限流阈值] 剩余可发 4 条 | 距离限制还有 9h30m\n" + fixedMessageID
	if got := client.messages; len(got) != 1 || got[0] != want {
		t.Fatalf("sent messages = %#v, want [%q]", got, want)
	}
}

func TestSendTextRejectsFrozenBeforeSendingUserText(t *testing.T) {
	store := config.NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if err := store.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}
	if err := store.AddBot(config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}); err != nil {
		t.Fatalf("AddBot() error = %v", err)
	}

	client := &fakeClient{}
	a := &App{
		store:  store,
		client: client,
		guard: &fakeGuard{
			reservation: protection.RejectNormal(protection.ReasonCount),
		},
		reminderText: "reminder",
	}

	err := a.SendText("bot-1", "hello")
	if err == nil {
		t.Fatal("SendText() error = nil, want protection rejection")
	}
	if !strings.Contains(err.Error(), "protection mode locked") {
		t.Fatalf("SendText() error = %q, want protection lock message", err)
	}
	if len(client.messages) != 0 {
		t.Fatalf("sent messages = %#v, want none", client.messages)
	}
}

func TestSendTextReleasesReservationWhenSendFails(t *testing.T) {
	store := config.NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if err := store.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}
	if err := store.AddBot(config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}); err != nil {
		t.Fatalf("AddBot() error = %v", err)
	}

	client := &fakeClient{sendErr: errors.New("remote down")}
	guard := &fakeGuard{reservation: protection.SendNormalThenReminder(protection.ReasonCount)}
	a := &App{
		store:        store,
		client:       client,
		guard:        guard,
		idGenerator:  fixedIDGenerator,
		reminderText: "reminder",
	}

	err := a.SendText("bot-1", "hello")
	if err == nil {
		t.Fatal("SendText() error = nil, want send failure")
	}
	if guard.releaseCalls != 1 {
		t.Fatalf("ReleaseNormalSend calls = %d, want 1", guard.releaseCalls)
	}
	if guard.recordReminderCalls != 0 {
		t.Fatalf("RecordReminderSend calls = %d, want 0", guard.recordReminderCalls)
	}
}

func TestCheckProtectionTimeWindowOnceSendsReminder(t *testing.T) {
	store := config.NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if err := store.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}
	if err := store.AddBot(config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}); err != nil {
		t.Fatalf("AddBot() error = %v", err)
	}

	client := &fakeClient{}
	guard := &fakeGuard{
		checkDecision: protection.SendReminderAndFreeze(protection.ReasonTime),
	}
	a := &App{
		store:        store,
		client:       client,
		guard:        guard,
		reminderText: "reminder",
	}

	if ok := a.checkProtectionTimeWindowOnce("bot-1"); !ok {
		t.Fatal("checkProtectionTimeWindowOnce() = false, want true")
	}
	if len(client.messages) != 1 || client.messages[0] != "reminder" {
		t.Fatalf("sent messages = %#v, want [reminder]", client.messages)
	}
	if guard.recordReminderCalls != 1 {
		t.Fatalf("RecordReminderSend calls = %d, want 1", guard.recordReminderCalls)
	}
}

func TestProtectionCheckerLifecycle(t *testing.T) {
	store := config.NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if err := store.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}
	if err := store.AddBot(config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}); err != nil {
		t.Fatalf("AddBot() error = %v", err)
	}

	a := &App{
		store:                     store,
		client:                    &fakeClient{},
		guard:                     &fakeGuard{},
		protectionEnabled:         false,
		timeCheckInterval:         time.Hour,
		runningProtectionCheckers: make(map[string]*protectionChecker),
	}

	a.startProtectionChecker("bot-1")
	if got := len(a.runningProtectionCheckers); got != 0 {
		t.Fatalf("runningProtectionCheckers = %d, want 0 when disabled", got)
	}

	a.protectionEnabled = true
	a.startProtectionChecker("bot-1")
	a.startProtectionChecker("bot-1")
	if got := len(a.runningProtectionCheckers); got != 1 {
		t.Fatalf("runningProtectionCheckers = %d, want 1", got)
	}

	a.stopProtectionCheckers()
	if got := len(a.runningProtectionCheckers); got != 0 {
		t.Fatalf("runningProtectionCheckers = %d, want 0 after stop", got)
	}
}

type fakeClient struct {
	mu                   sync.Mutex
	messages             []string
	sendErr              error
	afterSend            func(text string)
	sendStarted          chan struct{}
	sendStartedOnce      sync.Once
	waitForContextCancel bool
}

func (f *fakeClient) QRLoginWithWriter(_ io.Writer) (*config.UserConfig, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeClient) GetUpdates(config.UserConfig, time.Duration) (*ilink.UpdatesResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeClient) SendMessageContext(ctx context.Context, _ config.UserConfig, _ string, text string, _ string) error {
	if f.sendStarted != nil {
		f.sendStartedOnce.Do(func() {
			close(f.sendStarted)
		})
	}
	if f.waitForContextCancel {
		<-ctx.Done()
		return ctx.Err()
	}
	if f.sendErr != nil {
		return f.sendErr
	}
	f.mu.Lock()
	f.messages = append(f.messages, text)
	f.mu.Unlock()
	if f.afterSend != nil {
		f.afterSend(text)
	}
	return nil
}

func (f *fakeClient) SendTyping(config.UserConfig, int) error {
	return nil
}

func (f *fakeClient) SendTypingContext(context.Context, config.UserConfig, int) error {
	return nil
}

func fixedIDGenerator() (string, error) {
	return fixedMessageID, nil
}

type fakeGuard struct {
	reservation         protection.Reservation
	reservations        []protection.Reservation
	reserveErr          error
	reserveCalls        int
	releaseCalls        int
	failCanceledRelease bool
	releaseErr          error
	checkDecision       protection.Decision
	checkErr            error
	recordReminderCalls int
	activeCalls         int
}

func (f *fakeGuard) ReserveNormalSend(context.Context, string) (protection.Reservation, error) {
	f.reserveCalls++
	if f.reserveErr != nil {
		return protection.Reservation{}, f.reserveErr
	}
	if len(f.reservations) > 0 {
		reservation := f.reservations[0]
		f.reservations = f.reservations[1:]
		return reservation, nil
	}
	if f.reservation.Kind != protection.ReservationSendNormal || f.reservation.Reason != "" || f.reservation.HasStatus {
		return f.reservation, nil
	}
	return protection.SendNormal(), nil
}

func (f *fakeGuard) ReleaseNormalSend(ctx context.Context, _ string) error {
	f.releaseCalls++
	if f.failCanceledRelease && ctx.Err() != nil {
		return ctx.Err()
	}
	if f.releaseErr != nil {
		return f.releaseErr
	}
	return nil
}

func (f *fakeGuard) RecordReminderSend(context.Context, string) error {
	f.recordReminderCalls++
	return nil
}

func (f *fakeGuard) RecordActiveConversation(context.Context, string) error {
	f.activeCalls++
	return nil
}

func (f *fakeGuard) CheckTimeWindow(context.Context, string) (protection.Decision, error) {
	if f.checkErr != nil {
		return protection.Decision{}, f.checkErr
	}
	if f.checkDecision.Kind != protection.DecisionAllow {
		return f.checkDecision, nil
	}
	return protection.Allow(), nil
}
