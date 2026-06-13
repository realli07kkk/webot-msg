package runtimeconfig

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/realli07kkk/webot-msg/internal/ilink"
)

const (
	DefaultPort                = 26322
	DefaultConfigPath          = "~/.webot-msg/config/webot-msg.toml"
	DefaultAuthPath            = "~/.webot-msg/config/auth.json"
	DefaultControlSocketPath   = "~/.webot-msg/webot-msg.sock"
	DefaultProtectionStatePath = "~/.webot-msg/state/protection.json"
	DefaultAuditStatePath      = "~/.webot-msg/state/audit.json"
	DefaultLogPath             = "~/.webot-msg/logs/webot-msg.log"
	DefaultLogMaxSize          = "100MB"
	DefaultRedisKeyPrefix      = "webot-msg"
	DefaultActiveWindow        = "24h"
	DefaultTimeWarningBefore   = "30m"
	DefaultTimeCheckInterval   = "1m"
	DefaultAuditTTL            = "24h"
	DefaultReminderText        = "webot-msg 保护模式提醒：即将达到微信主动对话限制，请从微信 App 给机器人发一条消息后再继续发送。"
	DefaultTelemetryProtocol   = "grpc"
	DefaultTelemetryService    = "webot-msg"
	LegacyAuthPath             = "./config/auth.json"
)

var userHomeDir = os.UserHomeDir

type Config struct {
	API                 APIConfig              `toml:"api"`
	Storage             StorageConfig          `toml:"storage"`
	Control             ControlConfig          `toml:"control"`
	Ilink               IlinkConfig            `toml:"ilink"`
	Log                 LogConfig              `toml:"log"`
	Telemetry           TelemetryConfig        `toml:"telemetry"`
	Audit               AuditConfig            `toml:"audit"`
	AuditStatePath      string                 `toml:"-"`
	Protection          ProtectionConfig       `toml:"-"`
	ProtectionStatePath string                 `toml:"-"`
	LegacyProtection    LegacyProtectionConfig `toml:"protection"`
	Redis               RedisConfig            `toml:"redis"`
}

type APIConfig struct {
	Port int `toml:"port"`
}

type StorageConfig struct {
	AuthPath string `toml:"auth_path"`
}

type ControlConfig struct {
	SocketPath string `toml:"socket_path"`
}

type IlinkConfig struct {
	BaseURL string `toml:"base_url"`
}

type LogConfig struct {
	FilePath     string `toml:"file_path"`
	MaxSize      string `toml:"max_size"`
	MaxSizeBytes int64  `toml:"-"`
}

type TelemetryConfig struct {
	Endpoint           string            `toml:"endpoint"`
	Protocol           string            `toml:"protocol"`
	Insecure           bool              `toml:"insecure"`
	ServiceName        string            `toml:"service_name"`
	Headers            map[string]string `toml:"headers"`
	ResourceAttributes map[string]string `toml:"resource_attributes"`
}

type AuditConfig struct {
	TimeTTL         string        `toml:"time_ttl"`
	BodyTTL         string        `toml:"body_ttl"`
	TimeTTLDuration time.Duration `toml:"-"`
	BodyTTLDuration time.Duration `toml:"-"`
}

type ProtectionConfig struct {
	Enabled                   bool
	MessageLimit              int
	MessageWarningRemaining   int
	ActiveWindow              string
	TimeWarningBefore         string
	TimeCheckInterval         string
	ReminderText              string
	ActiveWindowDuration      time.Duration
	TimeWarningBeforeDuration time.Duration
	TimeCheckIntervalDuration time.Duration
}

type LegacyProtectionConfig struct {
	Enabled                 bool   `toml:"enabled"`
	MessageLimit            int    `toml:"message_limit"`
	MessageWarningRemaining int    `toml:"message_warning_remaining"`
	ActiveWindow            string `toml:"active_window"`
	TimeWarningBefore       string `toml:"time_warning_before"`
	TimeCheckInterval       string `toml:"time_check_interval"`
	ReminderText            string `toml:"reminder_text"`
}

type RedisConfig struct {
	URL       string `toml:"url"`
	Password  string `toml:"password"`
	KeyPrefix string `toml:"key_prefix"`
}

func (c Config) HasLegacyProtectionSettings() bool {
	legacy := c.LegacyProtection
	return legacy.Enabled ||
		legacy.MessageLimit != 0 ||
		legacy.MessageWarningRemaining != 0 ||
		strings.TrimSpace(legacy.ActiveWindow) != "" ||
		strings.TrimSpace(legacy.TimeWarningBefore) != "" ||
		strings.TrimSpace(legacy.TimeCheckInterval) != "" ||
		strings.TrimSpace(legacy.ReminderText) != ""
}

func Default() Config {
	return Config{
		API: APIConfig{
			Port: DefaultPort,
		},
		Storage: StorageConfig{
			AuthPath: DefaultAuthPath,
		},
		Control: ControlConfig{
			SocketPath: DefaultControlSocketPath,
		},
		Ilink: IlinkConfig{
			BaseURL: ilink.DefaultBaseURL,
		},
		Log: LogConfig{
			FilePath: DefaultLogPath,
			MaxSize:  DefaultLogMaxSize,
		},
		Telemetry: TelemetryConfig{
			Protocol:           DefaultTelemetryProtocol,
			ServiceName:        DefaultTelemetryService,
			Headers:            map[string]string{},
			ResourceAttributes: map[string]string{},
		},
		Audit: AuditConfig{
			TimeTTL: DefaultAuditTTL,
			BodyTTL: DefaultAuditTTL,
		},
		AuditStatePath: DefaultAuditStatePath,
		Protection: ProtectionConfig{
			Enabled:                 false,
			MessageLimit:            10,
			MessageWarningRemaining: 1,
			ActiveWindow:            DefaultActiveWindow,
			TimeWarningBefore:       DefaultTimeWarningBefore,
			TimeCheckInterval:       DefaultTimeCheckInterval,
			ReminderText:            DefaultReminderText,
		},
		ProtectionStatePath: DefaultProtectionStatePath,
		Redis: RedisConfig{
			KeyPrefix: DefaultRedisKeyPrefix,
		},
	}
}

func LoadFile(path string) (Config, error) {
	resolvedPath, err := expandHome(path)
	if err != nil {
		return Config{}, fmt.Errorf("config path: %w", err)
	}

	cfg := Default()
	meta, err := toml.DecodeFile(resolvedPath, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("load runtime config %s: %w", resolvedPath, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, key := range undecoded {
			keys = append(keys, key.String())
		}
		return Config{}, fmt.Errorf("unknown runtime config key(s): %s", strings.Join(keys, ", "))
	}
	return cfg, nil
}

func (c Config) Resolve() (Config, error) {
	resolved := c

	if resolved.API.Port < 1 || resolved.API.Port > 65535 {
		return Config{}, fmt.Errorf("api.port: must be between 1 and 65535")
	}

	baseURL, err := validateBaseURL(resolved.Ilink.BaseURL)
	if err != nil {
		return Config{}, fmt.Errorf("ilink.base_url: %w", err)
	}
	resolved.Ilink.BaseURL = baseURL

	authPath, err := expandHome(resolved.Storage.AuthPath)
	if err != nil {
		return Config{}, fmt.Errorf("storage.auth_path: %w", err)
	}
	if authPath == "" {
		return Config{}, fmt.Errorf("storage.auth_path: must not be empty")
	}
	resolved.Storage.AuthPath = authPath

	socketPath, err := expandHome(resolved.Control.SocketPath)
	if err != nil {
		return Config{}, fmt.Errorf("control.socket_path: %w", err)
	}
	if socketPath == "" {
		return Config{}, fmt.Errorf("control.socket_path: must not be empty")
	}
	resolved.Control.SocketPath = socketPath

	logPath, err := expandHome(resolved.Log.FilePath)
	if err != nil {
		return Config{}, fmt.Errorf("log.file_path: %w", err)
	}
	resolved.Log.FilePath = logPath

	if logPath != "" {
		sizeBytes, err := ParseSize(resolved.Log.MaxSize)
		if err != nil {
			return Config{}, fmt.Errorf("log.max_size: %w", err)
		}
		resolved.Log.MaxSizeBytes = sizeBytes
	}

	if err := resolveProtection(&resolved); err != nil {
		return Config{}, err
	}
	if err := resolveTelemetry(&resolved); err != nil {
		return Config{}, err
	}
	if err := resolveAudit(&resolved); err != nil {
		return Config{}, err
	}

	protectionStatePath, err := expandHome(resolved.ProtectionStatePath)
	if err != nil {
		return Config{}, fmt.Errorf("protection state path: %w", err)
	}
	if protectionStatePath == "" {
		return Config{}, fmt.Errorf("protection state path: must not be empty")
	}
	resolved.ProtectionStatePath = protectionStatePath

	auditStatePath, err := expandHome(resolved.AuditStatePath)
	if err != nil {
		return Config{}, fmt.Errorf("audit state path: %w", err)
	}
	if auditStatePath == "" {
		return Config{}, fmt.Errorf("audit state path: must not be empty")
	}
	resolved.AuditStatePath = auditStatePath

	return resolved, nil
}

func resolveProtection(cfg *Config) error {
	activeWindow, err := parsePositiveDuration(cfg.Protection.ActiveWindow)
	if err != nil {
		return fmt.Errorf("protection.active_window: %w", err)
	}
	timeWarningBefore, err := parsePositiveDuration(cfg.Protection.TimeWarningBefore)
	if err != nil {
		return fmt.Errorf("protection.time_warning_before: %w", err)
	}
	timeCheckInterval, err := parsePositiveDuration(cfg.Protection.TimeCheckInterval)
	if err != nil {
		return fmt.Errorf("protection.time_check_interval: %w", err)
	}
	if timeWarningBefore >= activeWindow {
		return fmt.Errorf("protection.time_warning_before: must be shorter than active_window")
	}
	cfg.Protection.ActiveWindowDuration = activeWindow
	cfg.Protection.TimeWarningBeforeDuration = timeWarningBefore
	cfg.Protection.TimeCheckIntervalDuration = timeCheckInterval

	if cfg.Protection.MessageLimit < 2 {
		return fmt.Errorf("protection.message_limit: must be at least 2")
	}
	if cfg.Protection.MessageWarningRemaining < 1 || cfg.Protection.MessageWarningRemaining >= cfg.Protection.MessageLimit {
		return fmt.Errorf("protection.message_warning_remaining: must be between 1 and message_limit-1")
	}
	if strings.TrimSpace(cfg.Protection.ReminderText) == "" {
		return fmt.Errorf("protection.reminder_text: must not be empty")
	}
	if cfg.Redis.KeyPrefix == "" {
		cfg.Redis.KeyPrefix = DefaultRedisKeyPrefix
	}
	return nil
}

func resolveTelemetry(cfg *Config) error {
	cfg.Telemetry.Endpoint = strings.TrimSpace(cfg.Telemetry.Endpoint)
	cfg.Telemetry.Protocol = strings.TrimSpace(cfg.Telemetry.Protocol)
	cfg.Telemetry.ServiceName = strings.TrimSpace(cfg.Telemetry.ServiceName)

	if cfg.Telemetry.Protocol == "" {
		cfg.Telemetry.Protocol = DefaultTelemetryProtocol
	}
	if cfg.Telemetry.ServiceName == "" {
		cfg.Telemetry.ServiceName = DefaultTelemetryService
	}
	if cfg.Telemetry.Protocol != "grpc" && cfg.Telemetry.Protocol != "http" {
		return fmt.Errorf("telemetry.protocol: must be grpc or http")
	}
	if cfg.Telemetry.Endpoint != "" {
		if err := validateOTLPEndpoint(cfg.Telemetry.Endpoint); err != nil {
			return fmt.Errorf("telemetry.endpoint: %w", err)
		}
	}
	if cfg.Telemetry.Headers == nil {
		cfg.Telemetry.Headers = map[string]string{}
	}
	if cfg.Telemetry.ResourceAttributes == nil {
		cfg.Telemetry.ResourceAttributes = map[string]string{}
	}
	return nil
}

func resolveAudit(cfg *Config) error {
	timeTTL, err := parsePositiveDuration(cfg.Audit.TimeTTL)
	if err != nil {
		return fmt.Errorf("audit.time_ttl: %w", err)
	}
	bodyTTL, err := parsePositiveDuration(cfg.Audit.BodyTTL)
	if err != nil {
		return fmt.Errorf("audit.body_ttl: %w", err)
	}
	cfg.Audit.TimeTTLDuration = timeTTL
	cfg.Audit.BodyTTLDuration = bodyTTL
	return nil
}

func parsePositiveDuration(value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	return d, nil
}

func (c Config) PrepareStorage() (bool, error) {
	usesDefaultAuthPath, err := c.usesDefaultAuthPath()
	if err != nil {
		return false, err
	}

	if err := ensureParentDir(c.Storage.AuthPath, 0700, usesDefaultAuthPath); err != nil {
		return false, fmt.Errorf("storage.auth_path: %w", err)
	}
	if c.Log.FilePath != "" {
		if err := ensureParentDir(c.Log.FilePath, 0755, false); err != nil {
			return false, fmt.Errorf("log.file_path: %w", err)
		}
	}
	usesDefaultControlSocketPath, err := c.usesDefaultControlSocketPath()
	if err != nil {
		return false, err
	}
	if err := ensureParentDir(c.Control.SocketPath, 0700, usesDefaultControlSocketPath); err != nil {
		return false, fmt.Errorf("control.socket_path: %w", err)
	}

	copied, err := c.copyLegacyAuth()
	if err != nil {
		return false, err
	}
	return copied, nil
}

func (c Config) copyLegacyAuth() (bool, error) {
	usesDefaultAuthPath, err := c.usesDefaultAuthPath()
	if err != nil {
		return false, err
	}
	if !usesDefaultAuthPath {
		return false, nil
	}

	if _, err := os.Stat(c.Storage.AuthPath); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("storage.auth_path: stat target: %w", err)
	}

	if _, err := os.Stat(LegacyAuthPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("legacy auth path: %w", err)
	}

	if err := copyFile(c.Storage.AuthPath, LegacyAuthPath); err != nil {
		return false, fmt.Errorf("copy legacy auth store: %w", err)
	}
	return true, nil
}

func (c Config) usesDefaultAuthPath() (bool, error) {
	defaultCfg, err := Default().Resolve()
	if err != nil {
		return false, err
	}
	return filepath.Clean(c.Storage.AuthPath) == filepath.Clean(defaultCfg.Storage.AuthPath), nil
}

func (c Config) usesDefaultControlSocketPath() (bool, error) {
	defaultCfg, err := Default().Resolve()
	if err != nil {
		return false, err
	}
	return filepath.Clean(c.Control.SocketPath) == filepath.Clean(defaultCfg.Control.SocketPath), nil
}

func ensureParentDir(path string, perm os.FileMode, forcePerm bool) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, perm); err != nil {
		return err
	}
	if dir == "." || !forcePerm {
		return nil
	}
	return os.Chmod(dir, perm)
}

func copyFile(dst string, src string) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func expandHome(path string) (string, error) {
	if path == "" {
		return path, nil
	}
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}

	home, err := userHomeDir()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", fmt.Errorf("home directory is empty")
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}

func validateBaseURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("must not be empty")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("must include scheme and host")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("scheme must be http or https")
	}
	return strings.TrimRight(value, "/"), nil
}

func validateOTLPEndpoint(value string) error {
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf("must be host:port")
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("host must not be empty")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

func validateRedisURL(value string, password string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("redis.url: must not be empty")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("redis.url: invalid URL")
	}
	if parsed.Scheme != "redis" && parsed.Scheme != "rediss" {
		return "", fmt.Errorf("redis.url: scheme must be redis or rediss")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("redis.url: host must not be empty")
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword && password != "" {
			return "", fmt.Errorf("redis.password: must not be set when redis.url already contains a password")
		}
	}
	return strings.TrimRight(value, "/"), nil
}

func ParseSize(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("must not be empty")
	}

	i := 0
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("must start with a positive integer")
	}

	number, err := strconv.ParseInt(value[:i], 10, 64)
	if err != nil {
		return 0, err
	}
	if number <= 0 {
		return 0, fmt.Errorf("must be greater than zero")
	}

	unit := strings.ToUpper(strings.TrimSpace(value[i:]))
	if unit == "" {
		unit = "B"
	}

	multiplier, ok := map[string]int64{
		"B":  1,
		"KB": 1024,
		"MB": 1024 * 1024,
		"GB": 1024 * 1024 * 1024,
		"TB": 1024 * 1024 * 1024 * 1024,
	}[unit]
	if !ok {
		return 0, fmt.Errorf("unsupported unit %q", unit)
	}
	if number > (1<<63-1)/multiplier {
		return 0, fmt.Errorf("value is too large")
	}
	return number * multiplier, nil
}
