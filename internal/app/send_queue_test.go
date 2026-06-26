package app

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/protection"
)

func TestDrainSendQueueReplaysFIFOAndClears(t *testing.T) {
	enqueuedOne := time.Now().UnixMilli()
	enqueuedTwo := time.Now().UnixMilli()
	guard := &fakeSendQueueGuard{
		queue: []fakeQueuedMessage{
			{text: "one", enqueuedMs: enqueuedOne},
			{text: "two", enqueuedMs: enqueuedTwo},
		},
	}
	client := &fakeClient{}
	a := newSendQueueTestApp(t, guard, client, config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	})

	a.drainSendQueue(context.Background(), "bot-1")

	wantOne := buildBacklogReplayNotice(enqueuedOne) + "\none\n" + fixedMessageID
	wantTwo := buildBacklogReplayNotice(enqueuedTwo) + "\ntwo\n" + fixedMessageID
	if got := client.messages; len(got) != 2 || got[0] != wantOne || got[1] != wantTwo {
		t.Fatalf("messages = %#v, want [%q %q]", got, wantOne, wantTwo)
	}
	if got := len(guard.queue); got != 0 {
		t.Fatalf("queue length = %d, want 0", got)
	}
	if guard.dropCalls != 2 {
		t.Fatalf("DropFront calls = %d, want 2", guard.dropCalls)
	}
}

func TestBuildBacklogReplayNoticeIncludesOriginalTimeAndReason(t *testing.T) {
	enqueuedMs := time.Date(2026, 6, 26, 2, 30, 45, 123*int(time.Millisecond), time.UTC).UnixMilli()

	got := buildBacklogReplayNotice(enqueuedMs)

	wantTime := "2026-06-26 10:30:45 +0800"
	if !strings.HasPrefix(got, backlogReplayPrefix+" 收到 API 调用时间："+wantTime+"；") {
		t.Fatalf("notice = %q, want prefix with original API time %q", got, wantTime)
	}
	if !strings.Contains(got, "因发送保护限制进入积压队列，恢复后延迟补发") {
		t.Fatalf("notice = %q, want backlog delay reason", got)
	}
}

func TestDrainSendQueueStopsOnRejectionAndKeepsFront(t *testing.T) {
	enqueuedOne := time.Now().UnixMilli()
	guard := &fakeSendQueueGuard{
		fakeGuard: fakeGuard{
			reservations: []protection.Reservation{
				protection.SendNormal(),
				protection.RejectNormal(protection.ReasonCount),
			},
		},
		queue: []fakeQueuedMessage{
			{text: "one", enqueuedMs: enqueuedOne},
			{text: "two", enqueuedMs: time.Now().UnixMilli()},
		},
	}
	client := &fakeClient{}
	a := newSendQueueTestApp(t, guard, client, config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	})

	a.drainSendQueue(context.Background(), "bot-1")

	wantOne := buildBacklogReplayNotice(enqueuedOne) + "\none\n" + fixedMessageID
	if got := client.messages; len(got) != 1 || got[0] != wantOne {
		t.Fatalf("messages = %#v, want [%q]", got, wantOne)
	}
	if got := len(guard.queue); got != 1 || guard.queue[0].text != "two" {
		t.Fatalf("queue = %#v, want front two retained", guard.queue)
	}
	if guard.dropCalls != 1 {
		t.Fatalf("DropFront calls = %d, want 1", guard.dropCalls)
	}
}

func TestDrainSendQueueDropsExpiredPayload(t *testing.T) {
	guard := &fakeSendQueueGuard{
		queue: []fakeQueuedMessage{
			{text: "old", enqueuedMs: time.Now().Add(-2 * time.Hour).UnixMilli()},
		},
	}
	client := &fakeClient{}
	a := newSendQueueTestApp(t, guard, client, config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	})
	a.protectionConfig.QueueTTL = time.Hour

	a.drainSendQueue(context.Background(), "bot-1")

	if len(client.messages) != 0 {
		t.Fatalf("messages = %#v, want none", client.messages)
	}
	if got := len(guard.queue); got != 0 {
		t.Fatalf("queue length = %d, want 0", got)
	}
	if guard.dropCalls != 1 {
		t.Fatalf("DropFront calls = %d, want 1", guard.dropCalls)
	}
}

func TestDrainSendQueueStopsWhenContextMissing(t *testing.T) {
	guard := &fakeSendQueueGuard{
		queue: []fakeQueuedMessage{
			{text: "one", enqueuedMs: time.Now().UnixMilli()},
		},
	}
	client := &fakeClient{}
	a := newSendQueueTestApp(t, guard, client, config.UserConfig{BotID: "bot-1"})

	a.drainSendQueue(context.Background(), "bot-1")

	if len(client.messages) != 0 {
		t.Fatalf("messages = %#v, want none", client.messages)
	}
	if got := len(guard.queue); got != 1 {
		t.Fatalf("queue length = %d, want 1 retained", got)
	}
	if guard.dropCalls != 0 {
		t.Fatalf("DropFront calls = %d, want 0", guard.dropCalls)
	}
}

func TestDrainSendQueuePopsSentMessageAfterDrainContextCanceled(t *testing.T) {
	enqueuedOne := time.Now().UnixMilli()
	guard := &fakeSendQueueGuard{
		failCanceledDrop: true,
		queue: []fakeQueuedMessage{
			{text: "one", enqueuedMs: enqueuedOne},
			{text: "two", enqueuedMs: time.Now().UnixMilli()},
		},
	}
	client := &fakeClient{}
	a := newSendQueueTestApp(t, guard, client, config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	})
	ctx, cancel := context.WithCancel(context.Background())
	client.afterSend = func(text string) {
		if strings.Contains(text, "\none\n") {
			cancel()
		}
	}

	a.drainSendQueue(ctx, "bot-1")

	client.mu.Lock()
	messages := append([]string(nil), client.messages...)
	client.mu.Unlock()
	wantOne := buildBacklogReplayNotice(enqueuedOne) + "\none\n" + fixedMessageID
	if len(messages) != 1 || messages[0] != wantOne {
		t.Fatalf("messages = %#v, want [%q]", messages, wantOne)
	}
	guard.mu.Lock()
	queue := append([]fakeQueuedMessage(nil), guard.queue...)
	guard.mu.Unlock()
	if len(queue) != 1 || queue[0].text != "two" {
		t.Fatalf("queue = %#v, want first sent message popped and second retained", queue)
	}
	if guard.dropCalls != 1 {
		t.Fatalf("DropFront calls = %d, want 1", guard.dropCalls)
	}
}

func TestDrainSendQueueReleasesReservationAfterSendContextCanceled(t *testing.T) {
	guard := &fakeSendQueueGuard{
		fakeGuard: fakeGuard{
			failCanceledRelease: true,
		},
		queue: []fakeQueuedMessage{
			{text: "one", enqueuedMs: time.Now().UnixMilli()},
		},
	}
	client := &fakeClient{
		sendStarted:          make(chan struct{}),
		waitForContextCancel: true,
	}
	a := newSendQueueTestApp(t, guard, client, config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		a.drainSendQueue(ctx, "bot-1")
		close(done)
	}()

	select {
	case <-client.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued send to start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for drain to stop")
	}

	if guard.releaseCalls != 1 {
		t.Fatalf("ReleaseNormalSend calls = %d, want 1", guard.releaseCalls)
	}
	if guard.dropCalls != 0 {
		t.Fatalf("DropFront calls = %d, want 0 after failed send", guard.dropCalls)
	}
	guard.mu.Lock()
	queue := append([]fakeQueuedMessage(nil), guard.queue...)
	guard.mu.Unlock()
	if len(queue) != 1 || queue[0].text != "one" {
		t.Fatalf("queue = %#v, want failed message retained", queue)
	}
}

func TestDisableProtectionStopsActiveSendQueueDrainerAndKeepsRemaining(t *testing.T) {
	enqueuedOne := time.Now().UnixMilli()
	guard := &fakeSendQueueGuard{
		failCanceledDrop: true,
		queue: []fakeQueuedMessage{
			{text: "one", enqueuedMs: enqueuedOne},
			{text: "two", enqueuedMs: time.Now().UnixMilli()},
		},
	}
	client := &fakeClient{}
	a := newSendQueueTestApp(t, guard, client, config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	})
	a.runtimeGuard = newEnabledRuntimeGuardForSendQueueTest(t)
	a.runningSendQueueDrainers = make(map[string]*sendQueueDrainer)

	disabled := make(chan error, 1)
	dropped := make(chan struct{})
	secondSent := make(chan struct{})
	var dropOnce sync.Once
	var secondOnce sync.Once
	guard.afterDrop = func() {
		dropOnce.Do(func() {
			close(dropped)
		})
	}
	client.afterSend = func(text string) {
		if strings.Contains(text, "\none\n") {
			disabled <- a.DisableProtection(io.Discard)
			return
		}
		if strings.Contains(text, "\ntwo\n") {
			secondOnce.Do(func() {
				close(secondSent)
			})
		}
	}

	a.startSendQueueDrainer("bot-1")

	select {
	case err := <-disabled:
		if err != nil {
			t.Fatalf("DisableProtection() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for DisableProtection")
	}
	select {
	case <-dropped:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first queued message to pop")
	}
	select {
	case <-secondSent:
		t.Fatal("second queued message was sent after protection disable")
	case <-time.After(50 * time.Millisecond):
	}

	client.mu.Lock()
	messages := append([]string(nil), client.messages...)
	client.mu.Unlock()
	wantOne := buildBacklogReplayNotice(enqueuedOne) + "\none\n" + fixedMessageID
	if len(messages) != 1 || messages[0] != wantOne {
		t.Fatalf("messages = %#v, want [%q]", messages, wantOne)
	}
	guard.mu.Lock()
	queue := append([]fakeQueuedMessage(nil), guard.queue...)
	guard.mu.Unlock()
	if len(queue) != 1 || queue[0].text != "two" {
		t.Fatalf("queue = %#v, want remaining second message", queue)
	}
	a.monitorMu.Lock()
	drainerCount := len(a.runningSendQueueDrainers)
	a.monitorMu.Unlock()
	if drainerCount != 0 {
		t.Fatalf("runningSendQueueDrainers = %d, want 0", drainerCount)
	}
}

func TestPrintProtectionStatusShowsQueuedMessages(t *testing.T) {
	a := &App{}
	var out bytes.Buffer

	a.printProtectionStatus(protection.Status{
		Enabled:                true,
		RedisConfigured:        true,
		BotID:                  "bot-1",
		ActiveWindowReady:      true,
		MessageLimit:           10,
		WarningThreshold:       9,
		MessagesBeforeReminder: 4,
		ActiveWindowRemaining:  time.Hour,
		TimeBeforeWarning:      30 * time.Minute,
		QueuedCount:            3,
	}, &out)

	if got := out.String(); !strings.Contains(got, "Queued messages: 3") {
		t.Fatalf("status output = %q, want queued messages line", got)
	}
}

func newSendQueueTestApp(t *testing.T, guard protection.Guard, client *fakeClient, user config.UserConfig) *App {
	t.Helper()

	store := config.NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if err := store.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}
	if err := store.AddBot(user); err != nil {
		t.Fatalf("AddBot() error = %v", err)
	}
	return &App{
		store:            store,
		client:           client,
		guard:            guard,
		idGenerator:      fixedIDGenerator,
		reminderText:     "reminder",
		protectionConfig: protection.EnableConfig{ActiveWindow: 24 * time.Hour},
	}
}

type fakeQueuedMessage struct {
	text       string
	enqueuedMs int64
}

type fakeSendQueueGuard struct {
	fakeGuard
	mu               sync.Mutex
	queue            []fakeQueuedMessage
	dropCalls        int
	failCanceledDrop bool
	afterDrop        func()
}

func (f *fakeSendQueueGuard) AcquireOrEnqueue(context.Context, string, string) (protection.Ingress, error) {
	return protection.Ingress{}, nil
}

func (f *fakeSendQueueGuard) PeekQueued(context.Context, string) (string, int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queue) == 0 {
		return "", 0, false, nil
	}
	return f.queue[0].text, f.queue[0].enqueuedMs, true, nil
}

func (f *fakeSendQueueGuard) DropFront(ctx context.Context, _ string) error {
	if f.failCanceledDrop && ctx.Err() != nil {
		return ctx.Err()
	}
	f.mu.Lock()
	if len(f.queue) > 0 {
		f.queue = f.queue[1:]
	}
	f.dropCalls++
	afterDrop := f.afterDrop
	f.mu.Unlock()
	if afterDrop != nil {
		afterDrop()
	}
	return nil
}

func (f *fakeSendQueueGuard) QueueLen(context.Context, string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.queue), nil
}

func newEnabledRuntimeGuardForSendQueueTest(t *testing.T) *protection.RuntimeGuard {
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
	return guard
}
