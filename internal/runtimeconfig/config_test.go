package runtimeconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realli07kkk/webot-msg/internal/ilink"
)

func TestLoadFileMergesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webot-msg.toml")
	if err := os.WriteFile(path, []byte("[api]\nport = 18080\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if cfg.API.Port != 18080 {
		t.Fatalf("API.Port = %d, want 18080", cfg.API.Port)
	}
	if cfg.Storage.AuthPath != DefaultAuthPath {
		t.Fatalf("Storage.AuthPath = %q, want %q", cfg.Storage.AuthPath, DefaultAuthPath)
	}
	if cfg.Ilink.BaseURL != ilink.DefaultBaseURL {
		t.Fatalf("Ilink.BaseURL = %q, want %q", cfg.Ilink.BaseURL, ilink.DefaultBaseURL)
	}
	if cfg.Log.FilePath != DefaultLogPath {
		t.Fatalf("Log.FilePath = %q, want %q", cfg.Log.FilePath, DefaultLogPath)
	}
	if cfg.Log.MaxSize != DefaultLogMaxSize {
		t.Fatalf("Log.MaxSize = %q, want %q", cfg.Log.MaxSize, DefaultLogMaxSize)
	}
}

func TestLoadFileRejectsUnknownKeys(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "unknown section",
			content: "[unknown]\nvalue = true\n",
			wantErr: "unknown.value",
		},
		{
			name:    "misspelled key",
			content: "[storage]\nauthpath = \"./config/auth.json\"\n",
			wantErr: "storage.authpath",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "webot-msg.toml")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			_, err := LoadFile(path)
			if err == nil {
				t.Fatal("LoadFile() error = nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("LoadFile() error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestResolveExpandsHomeAndParsesLogSize(t *testing.T) {
	home := t.TempDir()
	oldHomeDir := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = oldHomeDir })

	cfg := Default()
	cfg.Log.MaxSize = "1GB"

	resolved, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	wantAuthPath := filepath.Join(home, ".webot-msg", "config", "auth.json")
	if resolved.Storage.AuthPath != wantAuthPath {
		t.Fatalf("Storage.AuthPath = %q, want %q", resolved.Storage.AuthPath, wantAuthPath)
	}
	wantLogPath := filepath.Join(home, ".webot-msg", "logs", "webot-msg.log")
	if resolved.Log.FilePath != wantLogPath {
		t.Fatalf("Log.FilePath = %q, want %q", resolved.Log.FilePath, wantLogPath)
	}
	if resolved.Log.MaxSizeBytes != 1024*1024*1024 {
		t.Fatalf("Log.MaxSizeBytes = %d, want %d", resolved.Log.MaxSizeBytes, int64(1024*1024*1024))
	}
}

func TestPrepareStorageCopiesLegacyAuth(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	chdir(t, root)
	setHomeDir(t, home)

	if err := os.MkdirAll("config", 0755); err != nil {
		t.Fatalf("mkdir legacy config: %v", err)
	}
	if err := os.WriteFile(filepath.FromSlash("config/auth.json"), []byte(`{"bots":{}}`), 0644); err != nil {
		t.Fatalf("write legacy auth: %v", err)
	}

	cfg := Default()
	resolved, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	copied, err := resolved.PrepareStorage()
	if err != nil {
		t.Fatalf("PrepareStorage() error = %v", err)
	}
	if !copied {
		t.Fatal("PrepareStorage() copied = false, want true")
	}

	targetData, err := os.ReadFile(filepath.Join(home, ".webot-msg", "config", "auth.json"))
	if err != nil {
		t.Fatalf("read copied auth: %v", err)
	}
	if string(targetData) != `{"bots":{}}` {
		t.Fatalf("copied auth = %q", string(targetData))
	}
	assertPerm(t, filepath.Join(home, ".webot-msg", "config"), 0700)
	assertPerm(t, filepath.Join(home, ".webot-msg", "config", "auth.json"), 0600)
	if _, err := os.Stat(filepath.Join(home, ".webot-msg", "logs")); err != nil {
		t.Fatalf("stat log dir: %v", err)
	}
}

func TestPrepareStorageDoesNotOverwriteExistingAuth(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	chdir(t, root)
	setHomeDir(t, home)

	if err := os.MkdirAll("config", 0755); err != nil {
		t.Fatalf("mkdir legacy config: %v", err)
	}
	if err := os.WriteFile(filepath.FromSlash("config/auth.json"), []byte("legacy"), 0644); err != nil {
		t.Fatalf("write legacy auth: %v", err)
	}

	newAuthPath := filepath.Join(home, ".webot-msg", "config", "auth.json")
	if err := os.MkdirAll(filepath.Dir(newAuthPath), 0755); err != nil {
		t.Fatalf("mkdir new auth dir: %v", err)
	}
	if err := os.WriteFile(newAuthPath, []byte("new"), 0644); err != nil {
		t.Fatalf("write new auth: %v", err)
	}

	cfg := Default()
	resolved, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	copied, err := resolved.PrepareStorage()
	if err != nil {
		t.Fatalf("PrepareStorage() error = %v", err)
	}
	if copied {
		t.Fatal("PrepareStorage() copied = true, want false")
	}

	targetData, err := os.ReadFile(newAuthPath)
	if err != nil {
		t.Fatalf("read new auth: %v", err)
	}
	if string(targetData) != "new" {
		t.Fatalf("new auth = %q, want unchanged", string(targetData))
	}
}

func TestPrepareStorageKeepsExplicitLegacyAuthPath(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	chdir(t, root)
	setHomeDir(t, home)

	cfg := Default()
	cfg.Storage.AuthPath = filepath.FromSlash("./config/auth.json")
	resolved, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Storage.AuthPath != filepath.FromSlash("./config/auth.json") {
		t.Fatalf("Storage.AuthPath = %q, want explicit legacy path", resolved.Storage.AuthPath)
	}

	copied, err := resolved.PrepareStorage()
	if err != nil {
		t.Fatalf("PrepareStorage() error = %v", err)
	}
	if copied {
		t.Fatal("PrepareStorage() copied = true, want false for explicit legacy path")
	}
	assertPerm(t, filepath.FromSlash("./config"), 0700)
}

func TestResolveRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		update  func(*Config)
		wantErr string
	}{
		{
			name: "invalid port",
			update: func(cfg *Config) {
				cfg.API.Port = 0
			},
			wantErr: "api.port",
		},
		{
			name: "invalid base url",
			update: func(cfg *Config) {
				cfg.Ilink.BaseURL = "example.com"
			},
			wantErr: "ilink.base_url",
		},
		{
			name: "ftp base url",
			update: func(cfg *Config) {
				cfg.Ilink.BaseURL = "ftp://example.com"
			},
			wantErr: "ilink.base_url",
		},
		{
			name: "file base url",
			update: func(cfg *Config) {
				cfg.Ilink.BaseURL = "file://host"
			},
			wantErr: "ilink.base_url",
		},
		{
			name: "empty auth path",
			update: func(cfg *Config) {
				cfg.Storage.AuthPath = ""
			},
			wantErr: "storage.auth_path",
		},
		{
			name: "invalid log max size",
			update: func(cfg *Config) {
				cfg.Log.MaxSize = "10XB"
			},
			wantErr: "log.max_size",
		},
		{
			name: "home lookup error",
			update: func(cfg *Config) {
				oldHomeDir := userHomeDir
				userHomeDir = func() (string, error) { return "", fmt.Errorf("no home") }
				t.Cleanup(func() { userHomeDir = oldHomeDir })
			},
			wantErr: "storage.auth_path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.update(&cfg)

			_, err := cfg.Resolve()
			if err == nil {
				t.Fatal("Resolve() error = nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Resolve() error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s perm = %o, want %o", path, got, want)
	}
}

func setHomeDir(t *testing.T, home string) {
	t.Helper()
	oldHomeDir := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = oldHomeDir })
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		value string
		want  int64
	}{
		{value: "512", want: 512},
		{value: "1KB", want: 1024},
		{value: "10mb", want: 10 * 1024 * 1024},
		{value: "1 GB", want: 1024 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := ParseSize(tt.value)
			if err != nil {
				t.Fatalf("ParseSize(%q) error = %v", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("ParseSize(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}
