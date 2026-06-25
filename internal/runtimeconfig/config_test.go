package runtimeconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if cfg.Control.SocketPath != DefaultControlSocketPath {
		t.Fatalf("Control.SocketPath = %q, want %q", cfg.Control.SocketPath, DefaultControlSocketPath)
	}
	if cfg.Log.FilePath != DefaultLogPath {
		t.Fatalf("Log.FilePath = %q, want %q", cfg.Log.FilePath, DefaultLogPath)
	}
	if cfg.Log.MaxSize != DefaultLogMaxSize {
		t.Fatalf("Log.MaxSize = %q, want %q", cfg.Log.MaxSize, DefaultLogMaxSize)
	}
	if cfg.Protection.Enabled {
		t.Fatal("Protection.Enabled = true, want false")
	}
	if cfg.Protection.MessageLimit != 10 {
		t.Fatalf("Protection.MessageLimit = %d, want 10", cfg.Protection.MessageLimit)
	}
	if cfg.Protection.QueueMaxLen != DefaultProtectionQueueMax {
		t.Fatalf("Protection.QueueMaxLen = %d, want %d", cfg.Protection.QueueMaxLen, DefaultProtectionQueueMax)
	}
	if cfg.Redis.URL != "" {
		t.Fatalf("Redis.URL = %q, want empty default", cfg.Redis.URL)
	}
	if cfg.Redis.KeyPrefix != DefaultRedisKeyPrefix {
		t.Fatalf("Redis.KeyPrefix = %q, want %q", cfg.Redis.KeyPrefix, DefaultRedisKeyPrefix)
	}
	if cfg.Telemetry.Endpoint != "" {
		t.Fatalf("Telemetry.Endpoint = %q, want empty default", cfg.Telemetry.Endpoint)
	}
	if cfg.Telemetry.Protocol != DefaultTelemetryProtocol {
		t.Fatalf("Telemetry.Protocol = %q, want %q", cfg.Telemetry.Protocol, DefaultTelemetryProtocol)
	}
	if cfg.Telemetry.ServiceName != DefaultTelemetryService {
		t.Fatalf("Telemetry.ServiceName = %q, want %q", cfg.Telemetry.ServiceName, DefaultTelemetryService)
	}
	if len(cfg.Telemetry.Headers) != 0 {
		t.Fatalf("Telemetry.Headers = %#v, want empty default", cfg.Telemetry.Headers)
	}
	if len(cfg.Telemetry.ResourceAttributes) != 0 {
		t.Fatalf("Telemetry.ResourceAttributes = %#v, want empty default", cfg.Telemetry.ResourceAttributes)
	}
	if cfg.Audit.TimeTTL != DefaultAuditTTL {
		t.Fatalf("Audit.TimeTTL = %q, want %q", cfg.Audit.TimeTTL, DefaultAuditTTL)
	}
	if cfg.Audit.BodyTTL != DefaultAuditTTL {
		t.Fatalf("Audit.BodyTTL = %q, want %q", cfg.Audit.BodyTTL, DefaultAuditTTL)
	}
}

func TestLoadFileAcceptsAuditSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webot-msg.toml")
	content := `
[audit]
time_ttl = "2h"
body_ttl = "3h"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	resolved, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved.Audit.TimeTTLDuration != 2*time.Hour {
		t.Fatalf("Audit.TimeTTLDuration = %s, want 2h", resolved.Audit.TimeTTLDuration)
	}
	if resolved.Audit.BodyTTLDuration != 3*time.Hour {
		t.Fatalf("Audit.BodyTTLDuration = %s, want 3h", resolved.Audit.BodyTTLDuration)
	}
}

func TestLoadFileAcceptsTelemetrySection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webot-msg.toml")
	content := `
[telemetry]
endpoint = "collector.example.com:4317"
protocol = "http"
insecure = true
service_name = "custom-service"

[telemetry.headers]
Authorization = "Bearer secret"

[telemetry.resource_attributes]
token = "tencent-token"
env = "prod"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	resolved, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved.Telemetry.Endpoint != "collector.example.com:4317" {
		t.Fatalf("Telemetry.Endpoint = %q", resolved.Telemetry.Endpoint)
	}
	if resolved.Telemetry.Protocol != "http" {
		t.Fatalf("Telemetry.Protocol = %q, want http", resolved.Telemetry.Protocol)
	}
	if !resolved.Telemetry.Insecure {
		t.Fatal("Telemetry.Insecure = false, want true")
	}
	if resolved.Telemetry.ServiceName != "custom-service" {
		t.Fatalf("Telemetry.ServiceName = %q, want custom-service", resolved.Telemetry.ServiceName)
	}
	if resolved.Telemetry.Headers["Authorization"] != "Bearer secret" {
		t.Fatalf("Telemetry.Headers[Authorization] = %q", resolved.Telemetry.Headers["Authorization"])
	}
	if resolved.Telemetry.ResourceAttributes["token"] != "tencent-token" {
		t.Fatalf("Telemetry.ResourceAttributes[token] = %q", resolved.Telemetry.ResourceAttributes["token"])
	}
	if resolved.Telemetry.ResourceAttributes["env"] != "prod" {
		t.Fatalf("Telemetry.ResourceAttributes[env] = %q", resolved.Telemetry.ResourceAttributes["env"])
	}
}

func TestLoadFileAcceptsLegacyProtectionSectionWithoutEnabling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webot-msg.toml")
	content := `
[protection]
enabled = true
message_limit = 2
message_warning_remaining = 1
active_window = "1h"
time_warning_before = "10m"
time_check_interval = "5s"
reminder_text = "legacy"

[redis]
url = "redis://localhost:6379/0"
password = "secret"
key_prefix = "custom"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	resolved, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved.Protection.Enabled {
		t.Fatal("Protection.Enabled = true, want false")
	}
	if resolved.Protection.MessageLimit != 10 {
		t.Fatalf("Protection.MessageLimit = %d, want default 10", resolved.Protection.MessageLimit)
	}
	if resolved.Protection.ReminderText == "legacy" {
		t.Fatalf("Protection.ReminderText = %q, want internal default", resolved.Protection.ReminderText)
	}
	if resolved.Redis.URL != "redis://localhost:6379/0" {
		t.Fatalf("Redis.URL = %q, want redis://localhost:6379/0", resolved.Redis.URL)
	}
	if resolved.Redis.Password != "secret" {
		t.Fatalf("Redis.Password = %q, want secret", resolved.Redis.Password)
	}
	if resolved.Redis.KeyPrefix != "custom" {
		t.Fatalf("Redis.KeyPrefix = %q, want custom", resolved.Redis.KeyPrefix)
	}
	if !resolved.HasLegacyProtectionSettings() {
		t.Fatal("HasLegacyProtectionSettings() = false, want true")
	}
}

func TestDefaultHasNoLegacyProtectionSettings(t *testing.T) {
	if Default().HasLegacyProtectionSettings() {
		t.Fatal("HasLegacyProtectionSettings() = true, want false")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	if DefaultConfigPath != "~/.webot-msg/config/webot-msg.toml" {
		t.Fatalf("DefaultConfigPath = %q", DefaultConfigPath)
	}
	if DefaultProtectionStatePath != "~/.webot-msg/state/protection.json" {
		t.Fatalf("DefaultProtectionStatePath = %q", DefaultProtectionStatePath)
	}
	if DefaultAuditStatePath != "~/.webot-msg/state/audit.json" {
		t.Fatalf("DefaultAuditStatePath = %q", DefaultAuditStatePath)
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
		{
			name:    "unknown telemetry key",
			content: "[telemetry]\nendpiont = \"localhost:4317\"\n",
			wantErr: "telemetry.endpiont",
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

func TestResolveRejectsInvalidTelemetryConfig(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "invalid protocol",
			mutate: func(cfg *Config) {
				cfg.Telemetry.Endpoint = "localhost:4317"
				cfg.Telemetry.Protocol = "tcp"
			},
			wantErr: "telemetry.protocol",
		},
		{
			name: "invalid endpoint",
			mutate: func(cfg *Config) {
				cfg.Telemetry.Endpoint = "localhost"
			},
			wantErr: "telemetry.endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)
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
	wantSocketPath := filepath.Join(home, ".webot-msg", "webot-msg.sock")
	if resolved.Control.SocketPath != wantSocketPath {
		t.Fatalf("Control.SocketPath = %q, want %q", resolved.Control.SocketPath, wantSocketPath)
	}
	wantProtectionStatePath := filepath.Join(home, ".webot-msg", "state", "protection.json")
	if resolved.ProtectionStatePath != wantProtectionStatePath {
		t.Fatalf("ProtectionStatePath = %q, want %q", resolved.ProtectionStatePath, wantProtectionStatePath)
	}
	wantAuditStatePath := filepath.Join(home, ".webot-msg", "state", "audit.json")
	if resolved.AuditStatePath != wantAuditStatePath {
		t.Fatalf("AuditStatePath = %q, want %q", resolved.AuditStatePath, wantAuditStatePath)
	}
	if resolved.Log.MaxSizeBytes != 1024*1024*1024 {
		t.Fatalf("Log.MaxSizeBytes = %d, want %d", resolved.Log.MaxSizeBytes, int64(1024*1024*1024))
	}
	if resolved.Protection.ActiveWindowDuration != 24*time.Hour {
		t.Fatalf("Protection.ActiveWindowDuration = %s, want 24h", resolved.Protection.ActiveWindowDuration)
	}
	if resolved.Protection.TimeWarningBeforeDuration != 30*time.Minute {
		t.Fatalf("Protection.TimeWarningBeforeDuration = %s, want 30m", resolved.Protection.TimeWarningBeforeDuration)
	}
	if resolved.Protection.TimeCheckIntervalDuration != time.Minute {
		t.Fatalf("Protection.TimeCheckIntervalDuration = %s, want 1m", resolved.Protection.TimeCheckIntervalDuration)
	}
	if resolved.Protection.QueueTTLDuration != 24*time.Hour {
		t.Fatalf("Protection.QueueTTLDuration = %s, want 24h", resolved.Protection.QueueTTLDuration)
	}
	if resolved.Protection.QueueMaxLen != DefaultProtectionQueueMax {
		t.Fatalf("Protection.QueueMaxLen = %d, want %d", resolved.Protection.QueueMaxLen, DefaultProtectionQueueMax)
	}
	if resolved.Audit.TimeTTLDuration != 24*time.Hour {
		t.Fatalf("Audit.TimeTTLDuration = %s, want 24h", resolved.Audit.TimeTTLDuration)
	}
	if resolved.Audit.BodyTTLDuration != 24*time.Hour {
		t.Fatalf("Audit.BodyTTLDuration = %s, want 24h", resolved.Audit.BodyTTLDuration)
	}
}

func TestResolveKeepsRedisConfigWithoutProtectionEnabled(t *testing.T) {
	cfg := Default()
	cfg.Redis.URL = "redis://localhost:6379/0"
	cfg.Redis.Password = "secret"

	resolved, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Redis.URL != "redis://localhost:6379/0" {
		t.Fatalf("Redis.URL = %q, want redis://localhost:6379/0", resolved.Redis.URL)
	}
	if resolved.Redis.Password != "secret" {
		t.Fatalf("Redis.Password = %q, want secret", resolved.Redis.Password)
	}
	if resolved.Protection.Enabled {
		t.Fatal("Protection.Enabled = true, want false")
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
	assertPerm(t, filepath.Join(home, ".webot-msg"), 0700)
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

func TestPrepareStorageDoesNotChmodCustomControlSocketDir(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	customSocketDir := filepath.Join(root, "shared")
	chdir(t, root)
	setHomeDir(t, home)

	if err := os.MkdirAll(customSocketDir, 0755); err != nil {
		t.Fatalf("mkdir custom socket dir: %v", err)
	}

	cfg := Default()
	cfg.Control.SocketPath = filepath.Join(customSocketDir, "webot-msg.sock")
	resolved, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if _, err := resolved.PrepareStorage(); err != nil {
		t.Fatalf("PrepareStorage() error = %v", err)
	}

	assertPerm(t, customSocketDir, 0755)
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
			name: "empty control socket path",
			update: func(cfg *Config) {
				cfg.Control.SocketPath = ""
			},
			wantErr: "control.socket_path",
		},
		{
			name: "invalid log max size",
			update: func(cfg *Config) {
				cfg.Log.MaxSize = "10XB"
			},
			wantErr: "log.max_size",
		},
		{
			name: "invalid message limit",
			update: func(cfg *Config) {
				cfg.Protection.MessageLimit = 1
			},
			wantErr: "protection.message_limit",
		},
		{
			name: "invalid message warning remaining",
			update: func(cfg *Config) {
				cfg.Protection.MessageWarningRemaining = 10
			},
			wantErr: "protection.message_warning_remaining",
		},
		{
			name: "invalid active window",
			update: func(cfg *Config) {
				cfg.Protection.ActiveWindow = "bad"
			},
			wantErr: "protection.active_window",
		},
		{
			name: "time warning too long",
			update: func(cfg *Config) {
				cfg.Protection.TimeWarningBefore = "24h"
			},
			wantErr: "protection.time_warning_before",
		},
		{
			name: "empty reminder",
			update: func(cfg *Config) {
				cfg.Protection.ReminderText = ""
			},
			wantErr: "protection.reminder_text",
		},
		{
			name: "invalid audit time ttl",
			update: func(cfg *Config) {
				cfg.Audit.TimeTTL = "bad"
			},
			wantErr: "audit.time_ttl",
		},
		{
			name: "invalid audit body ttl",
			update: func(cfg *Config) {
				cfg.Audit.BodyTTL = "0s"
			},
			wantErr: "audit.body_ttl",
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
