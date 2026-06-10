package app

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/ilink"
	"github.com/realli07kkk/webot-msg/internal/protection"
)

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
		reminderText: "reminder",
	}

	if err := a.SendText("bot-1", "hello"); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	if len(client.messages) != 2 {
		t.Fatalf("sent messages = %#v, want normal + reminder", client.messages)
	}
	if client.messages[0] != "hello" || client.messages[1] != "reminder" {
		t.Fatalf("sent messages = %#v, want [hello reminder]", client.messages)
	}
	if guard.recordReminderCalls != 1 {
		t.Fatalf("RecordReminderSend calls = %d, want 1", guard.recordReminderCalls)
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

type fakeClient struct {
	messages []string
	sendErr  error
}

func (f *fakeClient) QRLoginWithWriter(_ io.Writer) (*config.UserConfig, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeClient) GetUpdates(config.UserConfig, time.Duration) (*ilink.UpdatesResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeClient) SendMessage(_ config.UserConfig, _ string, text string, _ string) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.messages = append(f.messages, text)
	return nil
}

func (f *fakeClient) SendTyping(config.UserConfig, int) error {
	return nil
}

type fakeGuard struct {
	reservation         protection.Reservation
	reserveErr          error
	releaseCalls        int
	checkDecision       protection.Decision
	checkErr            error
	recordReminderCalls int
	activeCalls         int
}

func (f *fakeGuard) ReserveNormalSend(context.Context, string) (protection.Reservation, error) {
	if f.reserveErr != nil {
		return protection.Reservation{}, f.reserveErr
	}
	if f.reservation.Kind != protection.ReservationSendNormal {
		return f.reservation, nil
	}
	return protection.SendNormal(), nil
}

func (f *fakeGuard) ReleaseNormalSend(context.Context, string) error {
	f.releaseCalls++
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
