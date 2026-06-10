package runtimeconfig

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/realli07kkk/webot-msg/internal/ilink"
)

const (
	DefaultPort       = 26322
	DefaultAuthPath   = "~/.webot-msg/config/auth.json"
	DefaultLogPath    = "~/.webot-msg/logs/webot-msg.log"
	DefaultLogMaxSize = "100MB"
	LegacyAuthPath    = "./config/auth.json"
)

var userHomeDir = os.UserHomeDir

type Config struct {
	API     APIConfig     `toml:"api"`
	Storage StorageConfig `toml:"storage"`
	Ilink   IlinkConfig   `toml:"ilink"`
	Log     LogConfig     `toml:"log"`
}

type APIConfig struct {
	Port int `toml:"port"`
}

type StorageConfig struct {
	AuthPath string `toml:"auth_path"`
}

type IlinkConfig struct {
	BaseURL string `toml:"base_url"`
}

type LogConfig struct {
	FilePath     string `toml:"file_path"`
	MaxSize      string `toml:"max_size"`
	MaxSizeBytes int64  `toml:"-"`
}

func Default() Config {
	return Config{
		API: APIConfig{
			Port: DefaultPort,
		},
		Storage: StorageConfig{
			AuthPath: DefaultAuthPath,
		},
		Ilink: IlinkConfig{
			BaseURL: ilink.DefaultBaseURL,
		},
		Log: LogConfig{
			FilePath: DefaultLogPath,
			MaxSize:  DefaultLogMaxSize,
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

	return resolved, nil
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
