package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/realli07kkk/webot-msg/internal/runtimeconfig"
)

func TestBuildRuntimeConfigUsesConfigPort(t *testing.T) {
	configPath := writeRuntimeConfig(t, "[api]\nport = 18080\n")
	setRuntimeConfigPath(t, configPath)

	cfg, err := buildRuntimeConfig("", false, runtimeconfig.DefaultPort, false)
	if err != nil {
		t.Fatalf("buildRuntimeConfig() error = %v", err)
	}

	if cfg.API.Port != 18080 {
		t.Fatalf("API.Port = %d, want 18080", cfg.API.Port)
	}
}

func TestBuildRuntimeConfigPortFlagOverridesConfig(t *testing.T) {
	configPath := writeRuntimeConfig(t, "[api]\nport = 18080\n")
	setRuntimeConfigPath(t, configPath)

	cfg, err := buildRuntimeConfig("", false, 19090, true)
	if err != nil {
		t.Fatalf("buildRuntimeConfig() error = %v", err)
	}

	if cfg.API.Port != 19090 {
		t.Fatalf("API.Port = %d, want 19090", cfg.API.Port)
	}
}

func TestParseCLIConsoleCommandBeforeFlags(t *testing.T) {
	opts, err := parseCLI([]string{"console"})
	if err != nil {
		t.Fatalf("parseCLI() error = %v", err)
	}

	if opts.command != "console" {
		t.Fatalf("command = %q, want console", opts.command)
	}
}

func TestParseCLIConsoleCommandAfterFlags(t *testing.T) {
	opts, err := parseCLI([]string{"-port", "19090", "console"})
	if err != nil {
		t.Fatalf("parseCLI() error = %v", err)
	}

	if opts.command != "console" {
		t.Fatalf("command = %q, want console", opts.command)
	}
	if !opts.portSet || opts.port != 19090 {
		t.Fatalf("portSet=%v port=%d, want true 19090", opts.portSet, opts.port)
	}
}

func TestBuildRuntimeConfigUsesExplicitConfigPath(t *testing.T) {
	defaultPath := writeRuntimeConfig(t, "[api]\nport = 18080\n")
	explicitPath := writeRuntimeConfig(t, "[api]\nport = 19090\n")
	setRuntimeConfigPath(t, defaultPath)

	cfg, err := buildRuntimeConfig(explicitPath, true, runtimeconfig.DefaultPort, false)
	if err != nil {
		t.Fatalf("buildRuntimeConfig() error = %v", err)
	}

	if cfg.API.Port != 19090 {
		t.Fatalf("API.Port = %d, want 19090", cfg.API.Port)
	}
}

func TestBuildRuntimeConfigFallsBackWhenDefaultConfigMissing(t *testing.T) {
	setRuntimeConfigPath(t, filepath.Join(t.TempDir(), "missing.toml"))

	cfg, err := buildRuntimeConfig("", false, runtimeconfig.DefaultPort, false)
	if err != nil {
		t.Fatalf("buildRuntimeConfig() error = %v", err)
	}

	if cfg.API.Port != runtimeconfig.DefaultPort {
		t.Fatalf("API.Port = %d, want %d", cfg.API.Port, runtimeconfig.DefaultPort)
	}
}

func TestParseCLIAcceptsConfigFlagForCompatibility(t *testing.T) {
	opts, err := parseCLI([]string{"-c", "config.toml"})
	if err != nil {
		t.Fatalf("parseCLI() error = %v", err)
	}

	if !opts.configSet || opts.configPath != "config.toml" {
		t.Fatalf("configSet=%v configPath=%q, want true config.toml", opts.configSet, opts.configPath)
	}
}

func TestParseCLIRejectsUnknownCommand(t *testing.T) {
	if _, err := parseCLI([]string{"login"}); err == nil {
		t.Fatal("parseCLI() error = nil, want error")
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
