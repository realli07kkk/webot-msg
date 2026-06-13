package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/realli07kkk/webot-msg/internal/audit"
	"github.com/realli07kkk/webot-msg/internal/config"
)

const secondMessageID = "01890f3e-6f44-7c2d-8d9e-123456789abc"

func TestAuditCommandsPersistStateAndControlRecording(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	defer redisServer.Close()

	statePath := filepath.Join(t.TempDir(), "state", "audit.json")
	store := newAppStoreWithBot(t)
	idGen := sequenceIDGenerator(fixedMessageID, secondMessageID)
	a := &App{
		store:           store,
		client:          &fakeClient{},
		guard:           &fakeGuard{},
		auditor:         audit.NewRecorder(),
		auditConfig:     auditEnableConfig(redisServer.Addr()),
		auditStateStore: audit.NewStateStore(statePath),
		idGenerator:     idGen,
		reminderText:    "reminder",
	}

	var out bytes.Buffer
	if err := a.EnableAudit(&out); err != nil {
		t.Fatalf("EnableAudit() error = %v", err)
	}
	state, err := audit.NewStateStore(statePath).Load()
	if err != nil {
		t.Fatalf("Load() after EnableAudit error = %v", err)
	}
	if !state.AuditEnabled {
		t.Fatal("AuditEnabled = false, want true after enable")
	}

	if err := a.SendText("bot-1", "hello"); err != nil {
		t.Fatalf("SendText() after EnableAudit error = %v", err)
	}
	bodyKey := "webot-msg:audit:body:" + fixedMessageID
	if got, err := redisServer.Get(bodyKey); err != nil || got != "hello\n"+fixedMessageID {
		t.Fatalf("audit body key = %q, %v; want final body", got, err)
	}

	out.Reset()
	if err := a.DisableAudit(&out); err != nil {
		t.Fatalf("DisableAudit() error = %v", err)
	}
	state, err = audit.NewStateStore(statePath).Load()
	if err != nil {
		t.Fatalf("Load() after DisableAudit error = %v", err)
	}
	if state.AuditEnabled {
		t.Fatal("AuditEnabled = true, want false after disable")
	}
	if err := a.SendText("bot-1", "bye"); err != nil {
		t.Fatalf("SendText() after DisableAudit error = %v", err)
	}
	if redisServer.Exists("webot-msg:audit:body:" + secondMessageID) {
		t.Fatal("audit body key for disabled send exists, want none")
	}
}

func TestAuditCommandsReturnPartialErrorWhenPersistFails(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	defer redisServer.Close()

	blockingFile := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(blockingFile, []byte("not a directory"), 0600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	recorder := audit.NewRecorder()
	a := &App{
		auditor:         recorder,
		auditConfig:     auditEnableConfig(redisServer.Addr()),
		auditStateStore: audit.NewStateStore(filepath.Join(blockingFile, "audit.json")),
	}

	var out bytes.Buffer
	err = a.EnableAudit(&out)
	if err == nil {
		t.Fatal("EnableAudit() error = nil, want partial-success persist error")
	}
	if !recorder.Enabled() {
		t.Fatal("auditor.Enabled() = false, want true after partial-success enable")
	}
	if got := err.Error(); !strings.Contains(got, "audit enabled for current process") || !strings.Contains(got, "persist audit state failed") {
		t.Fatalf("EnableAudit() error = %q, want partial-success persist message", got)
	}

	out.Reset()
	err = a.DisableAudit(&out)
	if err == nil {
		t.Fatal("DisableAudit() error = nil, want partial-success persist error")
	}
	if recorder.Enabled() {
		t.Fatal("auditor.Enabled() = true, want false after partial-success disable")
	}
	if got := err.Error(); !strings.Contains(got, "audit disabled for current process") || !strings.Contains(got, "persist audit state failed") {
		t.Fatalf("DisableAudit() error = %q, want partial-success persist message", got)
	}
}

func TestRestoreAuditStateEnablesRecorder(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	defer redisServer.Close()

	statePath := filepath.Join(t.TempDir(), "state", "audit.json")
	if err := audit.NewStateStore(statePath).Save(audit.PersistedState{AuditEnabled: true}); err != nil {
		t.Fatalf("Save() state error = %v", err)
	}
	recorder := audit.NewRecorder()
	a := New(Options{
		AuthPath:       filepath.Join(t.TempDir(), "auth.json"),
		Auditor:        recorder,
		AuditConfig:    auditEnableConfig(redisServer.Addr()),
		AuditStatePath: statePath,
	})

	a.restoreAuditState(nil)

	if !recorder.Enabled() {
		t.Fatal("auditor.Enabled() = false, want true after restore")
	}
}

func TestRestoreAuditStateFailureDoesNotRewriteState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state", "audit.json")
	if err := audit.NewStateStore(statePath).Save(audit.PersistedState{AuditEnabled: true}); err != nil {
		t.Fatalf("Save() state error = %v", err)
	}
	recorder := audit.NewRecorder()
	a := New(Options{
		AuthPath:       filepath.Join(t.TempDir(), "auth.json"),
		Auditor:        recorder,
		AuditConfig:    audit.EnableConfig{RedisURL: ""},
		AuditStatePath: statePath,
	})

	var out bytes.Buffer
	a.restoreAuditState(&out)

	if recorder.Enabled() {
		t.Fatal("auditor.Enabled() = true, want false")
	}
	if got := out.String(); !strings.Contains(got, "audit auto-restore failed") {
		t.Fatalf("restore warning output = %q, want restore failure warning", got)
	}
	state, err := audit.NewStateStore(statePath).Load()
	if err != nil {
		t.Fatalf("Load() after failed restore error = %v", err)
	}
	if !state.AuditEnabled {
		t.Fatal("AuditEnabled = false, want true after failed restore")
	}
}

func TestPrintAuditStatusShowsSwitchAndTTL(t *testing.T) {
	a := &App{
		auditor: audit.NewRecorder(),
		auditConfig: audit.EnableConfig{
			RedisURL:  "redis://127.0.0.1:6379/0",
			KeyPrefix: "webot-msg",
			TimeTTL:   time.Hour,
			BodyTTL:   2 * time.Hour,
		},
	}

	var out bytes.Buffer
	a.PrintAuditStatus(&out)

	got := out.String()
	for _, want := range []string{"Audit disabled.", "Redis configured: yes", "Time TTL: 1h0m0s", "Body TTL: 2h0m0s"} {
		if !strings.Contains(got, want) {
			t.Fatalf("PrintAuditStatus() = %q, want %q", got, want)
		}
	}
}

func newAppStoreWithBot(t *testing.T) *config.Store {
	t.Helper()

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
	return store
}

func sequenceIDGenerator(ids ...string) func() (string, error) {
	next := 0
	return func() (string, error) {
		if next >= len(ids) {
			return ids[len(ids)-1], nil
		}
		id := ids[next]
		next++
		return id, nil
	}
}

func auditEnableConfig(redisAddr string) audit.EnableConfig {
	return audit.EnableConfig{
		RedisURL:  "redis://" + redisAddr + "/0",
		KeyPrefix: "webot-msg",
		TimeTTL:   24 * time.Hour,
		BodyTTL:   24 * time.Hour,
	}
}
