package main

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realli07kkk/webot-msg/internal/runtimeconfig"
)

func TestBuildRuntimeConfigUsesConfigPort(t *testing.T) {
	configPath := writeRuntimeConfig(t, "[api]\nport = 18080\n")
	setRuntimeConfigPath(t, configPath)

	cfg, err := buildRuntimeConfig()
	if err != nil {
		t.Fatalf("buildRuntimeConfig() error = %v", err)
	}

	if cfg.API.Port != 18080 {
		t.Fatalf("API.Port = %d, want 18080", cfg.API.Port)
	}
}

func TestBuildRuntimeConfigFallsBackWhenDefaultConfigMissing(t *testing.T) {
	setRuntimeConfigPath(t, filepath.Join(t.TempDir(), "missing.toml"))

	cfg, err := buildRuntimeConfig()
	if err != nil {
		t.Fatalf("buildRuntimeConfig() error = %v", err)
	}

	if cfg.API.Port != runtimeconfig.DefaultPort {
		t.Fatalf("API.Port = %d, want %d", cfg.API.Port, runtimeconfig.DefaultPort)
	}
}

func TestMainRejectsArguments(t *testing.T) {
	if argsValue := os.Getenv("WEBOT_MSG_TEST_REJECT_ARGS"); argsValue != "" {
		os.Args = append([]string{"webot-msg"}, strings.Split(argsValue, "\t")...)
		main()
		return
	}

	tests := [][]string{
		{"serve"},
		{"console"},
		{"-port", "8080"},
		{"-c", "x.toml"},
		{"foo", "bar"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=TestMainRejectsArguments")
			cmd.Env = append(os.Environ(), "WEBOT_MSG_TEST_REJECT_ARGS="+strings.Join(args, "\t"))
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("main() error = %v, want exit error", err)
			}
			if exitErr.ExitCode() != 2 {
				t.Fatalf("exit code = %d, want 2", exitErr.ExitCode())
			}
			if stdout.String() != "" {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			want := "webot-msg does not accept arguments; run `webot-msg` without arguments"
			if got := strings.TrimSpace(stderr.String()); got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
		})
	}
}

func TestMainPrintsStartupErrorToStderrWithFileLogging(t *testing.T) {
	if os.Getenv("WEBOT_MSG_TEST_RUN_MAIN") == "1" {
		os.Args = []string{"webot-msg"}
		main()
		return
	}

	home, err := os.MkdirTemp("/tmp", "webot-msg-main-*")
	if err != nil {
		t.Fatalf("create temp home: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(home)
	})
	configDir := filepath.Join(home, ".webot-msg", "config")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	authPath := filepath.Join(configDir, "auth.json")
	authJSON := `{"bots":{"bot-1":{"bot_token":"token-1","bot_id":"bot-1","ilink_user_id":"user-1","context_token":"ctx-1","api_token":"api-1"}}}`
	if err := os.WriteFile(authPath, []byte(authJSON), 0600); err != nil {
		t.Fatalf("write auth store: %v", err)
	}

	socketPath := filepath.Join(home, ".webot-msg", "webot-msg.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	defer listener.Close()

	cmd := exec.Command(os.Args[0], "-test.run=TestMainPrintsStartupErrorToStderrWithFileLogging")
	cmd.Env = append(os.Environ(), "WEBOT_MSG_TEST_RUN_MAIN=1", "HOME="+home)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("main() error = %v, want exit error", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1", exitErr.ExitCode())
	}
	if !strings.Contains(stdout.String(), "Loaded 1 bots.") {
		t.Fatalf("stdout = %q, want loaded bots message", stdout.String())
	}
	if !strings.Contains(stderr.String(), "start control console failed: control socket already in use") {
		t.Fatalf("stderr = %q, want startup error", stderr.String())
	}
}

func TestLegacyProtectionWarning(t *testing.T) {
	if got := legacyProtectionWarning(runtimeconfig.Default()); got != "" {
		t.Fatalf("legacyProtectionWarning(default) = %q, want empty", got)
	}

	cfg := runtimeconfig.Default()
	cfg.LegacyProtection.Enabled = true
	got := legacyProtectionWarning(cfg)
	if !strings.Contains(got, "legacy [protection] config is ignored") {
		t.Fatalf("legacyProtectionWarning() = %q, want migration warning", got)
	}
	if !strings.Contains(got, "/protection enable") {
		t.Fatalf("legacyProtectionWarning() = %q, want /protection enable guidance", got)
	}
	if !strings.Contains(got, "once") {
		t.Fatalf("legacyProtectionWarning() = %q, want one-time enable guidance", got)
	}
}

func writeRuntimeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "webot-msg.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	return path
}

func setRuntimeConfigPath(t *testing.T, path string) {
	t.Helper()

	oldPath := runtimeConfigPath
	runtimeConfigPath = path
	t.Cleanup(func() {
		runtimeConfigPath = oldPath
	})
}
