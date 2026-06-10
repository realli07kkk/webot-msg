package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/realli07kkk/webot-msg/internal/runtimeconfig"
)

func TestBuildRuntimeConfigUsesConfigPort(t *testing.T) {
	configPath := writeRuntimeConfig(t, "[api]\nport = 18080\n")

	cfg, err := buildRuntimeConfig(configPath, runtimeconfig.DefaultPort, false)
	if err != nil {
		t.Fatalf("buildRuntimeConfig() error = %v", err)
	}

	if cfg.API.Port != 18080 {
		t.Fatalf("API.Port = %d, want 18080", cfg.API.Port)
	}
}

func TestBuildRuntimeConfigPortFlagOverridesConfig(t *testing.T) {
	configPath := writeRuntimeConfig(t, "[api]\nport = 18080\n")

	cfg, err := buildRuntimeConfig(configPath, 19090, true)
	if err != nil {
		t.Fatalf("buildRuntimeConfig() error = %v", err)
	}

	if cfg.API.Port != 19090 {
		t.Fatalf("API.Port = %d, want 19090", cfg.API.Port)
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
