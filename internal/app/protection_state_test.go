package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/realli07kkk/webot-msg/internal/protection"
)

func TestProtectionCommandsPersistState(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	defer redisServer.Close()

	statePath := filepath.Join(t.TempDir(), "state", "protection.json")
	guard := protection.NewRuntimeGuard()
	a := New(Options{
		AuthPath:            filepath.Join(t.TempDir(), "auth.json"),
		Guard:               guard,
		ProtectionConfig:    protectionEnableConfig(redisServer.Addr()),
		ProtectionStatePath: statePath,
		TimeCheckInterval:   time.Hour,
	})

	var out bytes.Buffer
	if err := a.EnableProtection(&out); err != nil {
		t.Fatalf("EnableProtection() error = %v", err)
	}
	state, err := protection.NewStateStore(statePath).Load()
	if err != nil {
		t.Fatalf("Load() after EnableProtection error = %v", err)
	}
	if !state.ProtectionEnabled {
		t.Fatal("ProtectionEnabled = false, want true after enable")
	}

	out.Reset()
	if err := a.DisableProtection(&out); err != nil {
		t.Fatalf("DisableProtection() error = %v", err)
	}
	state, err = protection.NewStateStore(statePath).Load()
	if err != nil {
		t.Fatalf("Load() after DisableProtection error = %v", err)
	}
	if state.ProtectionEnabled {
		t.Fatal("ProtectionEnabled = true, want false after disable")
	}
}

func TestProtectionCommandsWarnWhenPersistFails(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	defer redisServer.Close()

	blockingFile := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(blockingFile, []byte("not a directory"), 0600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	guard := protection.NewRuntimeGuard()
	a := New(Options{
		AuthPath:            filepath.Join(t.TempDir(), "auth.json"),
		Guard:               guard,
		ProtectionConfig:    protectionEnableConfig(redisServer.Addr()),
		ProtectionStatePath: filepath.Join(blockingFile, "protection.json"),
		TimeCheckInterval:   time.Hour,
	})

	var out bytes.Buffer
	if err := a.EnableProtection(&out); err != nil {
		t.Fatalf("EnableProtection() error = %v", err)
	}
	if !guard.Enabled() {
		t.Fatal("guard.Enabled() = false, want true after enable with persist failure")
	}
	if got := out.String(); !strings.Contains(got, "Warning: persist protection state failed") {
		t.Fatalf("EnableProtection output = %q, want persist warning", got)
	}

	out.Reset()
	if err := a.DisableProtection(&out); err != nil {
		t.Fatalf("DisableProtection() error = %v", err)
	}
	if guard.Enabled() {
		t.Fatal("guard.Enabled() = true, want false after disable with persist failure")
	}
	if got := out.String(); !strings.Contains(got, "Warning: persist protection state failed") {
		t.Fatalf("DisableProtection output = %q, want persist warning", got)
	}
}

func TestRestoreProtectionStateEnablesRuntimeGuard(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	defer redisServer.Close()

	statePath := filepath.Join(t.TempDir(), "state", "protection.json")
	if err := protection.NewStateStore(statePath).Save(protection.PersistedState{ProtectionEnabled: true}); err != nil {
		t.Fatalf("Save() state error = %v", err)
	}
	guard := protection.NewRuntimeGuard()
	a := New(Options{
		AuthPath:            filepath.Join(t.TempDir(), "auth.json"),
		Guard:               guard,
		ProtectionConfig:    protectionEnableConfig(redisServer.Addr()),
		ProtectionStatePath: statePath,
		TimeCheckInterval:   time.Hour,
	})

	a.restoreProtectionState(nil)

	if !guard.Enabled() {
		t.Fatal("guard.Enabled() = false, want true after restore")
	}
	status, err := guard.RuntimeStatus(context.Background(), "")
	if err != nil {
		t.Fatalf("RuntimeStatus() error = %v", err)
	}
	if !status.Enabled {
		t.Fatal("Status.Enabled = false, want true after restore")
	}
}

func TestRestoreProtectionStateDisabledOrMissingKeepsGuardDisabled(t *testing.T) {
	tests := []struct {
		name      string
		writeFile bool
		state     protection.PersistedState
	}{
		{name: "missing"},
		{name: "disabled", writeFile: true, state: protection.PersistedState{ProtectionEnabled: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "state", "protection.json")
			if tt.writeFile {
				if err := protection.NewStateStore(statePath).Save(tt.state); err != nil {
					t.Fatalf("Save() state error = %v", err)
				}
			}
			guard := protection.NewRuntimeGuard()
			a := New(Options{
				AuthPath:            filepath.Join(t.TempDir(), "auth.json"),
				Guard:               guard,
				ProtectionConfig:    protection.EnableConfig{RedisURL: ""},
				ProtectionStatePath: statePath,
				TimeCheckInterval:   time.Hour,
			})

			var out bytes.Buffer
			a.restoreProtectionState(&out)

			if guard.Enabled() {
				t.Fatal("guard.Enabled() = true, want false")
			}
			if got := out.String(); got != "" {
				t.Fatalf("restore warning output = %q, want empty", got)
			}
		})
	}
}

func TestRestoreProtectionStateWarnsForDamagedJSON(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state", "protection.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{bad json"), 0600); err != nil {
		t.Fatalf("write damaged state: %v", err)
	}
	guard := protection.NewRuntimeGuard()
	a := New(Options{
		AuthPath:            filepath.Join(t.TempDir(), "auth.json"),
		Guard:               guard,
		ProtectionConfig:    protectionEnableConfig("127.0.0.1:1"),
		ProtectionStatePath: statePath,
		TimeCheckInterval:   time.Hour,
	})

	var out bytes.Buffer
	a.restoreProtectionState(&out)

	if guard.Enabled() {
		t.Fatal("guard.Enabled() = true, want false")
	}
	if got := out.String(); !strings.Contains(got, "protection state file is unreadable") {
		t.Fatalf("restore warning output = %q, want damaged state warning", got)
	}
}

func TestRestoreProtectionStateFailureDoesNotRewriteState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state", "protection.json")
	if err := protection.NewStateStore(statePath).Save(protection.PersistedState{ProtectionEnabled: true}); err != nil {
		t.Fatalf("Save() state error = %v", err)
	}
	guard := protection.NewRuntimeGuard()
	a := New(Options{
		AuthPath:            filepath.Join(t.TempDir(), "auth.json"),
		Guard:               guard,
		ProtectionConfig:    protection.EnableConfig{RedisURL: ""},
		ProtectionStatePath: statePath,
		TimeCheckInterval:   time.Hour,
	})

	var out bytes.Buffer
	a.restoreProtectionState(&out)

	if guard.Enabled() {
		t.Fatal("guard.Enabled() = true, want false")
	}
	if got := out.String(); !strings.Contains(got, "protection auto-restore failed") {
		t.Fatalf("restore warning output = %q, want restore failure warning", got)
	}
	state, err := protection.NewStateStore(statePath).Load()
	if err != nil {
		t.Fatalf("Load() after failed restore error = %v", err)
	}
	if !state.ProtectionEnabled {
		t.Fatal("ProtectionEnabled = false, want true after failed restore")
	}
}

func protectionEnableConfig(redisAddr string) protection.EnableConfig {
	return protection.EnableConfig{
		RedisURL:                "redis://" + redisAddr + "/0",
		KeyPrefix:               "webot-msg",
		MessageLimit:            10,
		MessageWarningRemaining: 1,
		ActiveWindow:            24 * time.Hour,
		TimeWarningBefore:       30 * time.Minute,
	}
}
