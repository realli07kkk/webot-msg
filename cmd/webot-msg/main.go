package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"syscall"

	"github.com/realli07kkk/webot-msg/internal/app"
	"github.com/realli07kkk/webot-msg/internal/control"
	"github.com/realli07kkk/webot-msg/internal/logfile"
	"github.com/realli07kkk/webot-msg/internal/protection"
	"github.com/realli07kkk/webot-msg/internal/runtimeconfig"
	"golang.org/x/term"
)

func main() {
	if len(os.Args[1:]) > 0 {
		fmt.Fprintln(os.Stderr, "webot-msg does not accept arguments; run `webot-msg` without arguments")
		os.Exit(2)
	}

	resolved, err := buildRuntimeConfig()
	if err != nil {
		log.Fatal(err)
	}

	if attached, err := attachExistingConsole(resolved.Control.SocketPath); err != nil {
		log.Fatal(err)
	} else if attached {
		return
	}

	legacyAuthCopied, err := resolved.PrepareStorage()
	if err != nil {
		log.Fatal(err)
	}

	logWriter, err := logfile.NewSizeWriter(resolved.Log.FilePath, resolved.Log.MaxSizeBytes)
	if err != nil {
		log.Fatal(err)
	}
	if logWriter != nil {
		defer logWriter.Close()
		log.SetOutput(logWriter)
		log.Printf("Runtime config loaded: api_port=%d auth_path=%s control_socket_path=%s base_url=%s log_file=%s log_max_size_bytes=%d",
			resolved.API.Port, resolved.Storage.AuthPath, resolved.Control.SocketPath, resolved.Ilink.BaseURL, resolved.Log.FilePath, resolved.Log.MaxSizeBytes)
		if legacyAuthCopied {
			log.Printf("Legacy auth store copied: source=%s target=%s", runtimeconfig.LegacyAuthPath, resolved.Storage.AuthPath)
		}
	}
	warnLegacyProtectionSettings(resolved, logWriter != nil)

	guard := protection.NewRuntimeGuard()
	application := app.New(app.Options{
		AuthPath:          resolved.Storage.AuthPath,
		BaseURL:           resolved.Ilink.BaseURL,
		ControlSocketPath: resolved.Control.SocketPath,
		Guard:             guard,
		ProtectionConfig: protection.EnableConfig{
			RedisURL:                resolved.Redis.URL,
			RedisPassword:           resolved.Redis.Password,
			KeyPrefix:               resolved.Redis.KeyPrefix,
			MessageLimit:            resolved.Protection.MessageLimit,
			MessageWarningRemaining: resolved.Protection.MessageWarningRemaining,
			ActiveWindow:            resolved.Protection.ActiveWindowDuration,
			TimeWarningBefore:       resolved.Protection.TimeWarningBeforeDuration,
		},
		ProtectionEnabled:   guard.Enabled(),
		ProtectionStatePath: resolved.ProtectionStatePath,
		ReminderText:        resolved.Protection.ReminderText,
		TimeCheckInterval:   resolved.Protection.TimeCheckIntervalDuration,
	})
	if err := application.Run(resolved.API.Port); err != nil {
		if errors.Is(err, control.ErrSocketAlreadyInUse) {
			if attached, attachErr := attachExistingConsole(resolved.Control.SocketPath); attachErr != nil {
				err = fmt.Errorf("%w; attach existing console failed: %v", err, attachErr)
			} else if attached {
				return
			}
		}
		fatalStartupError(err, logWriter != nil)
	}
}

func attachExistingConsole(socketPath string) (bool, error) {
	attach := control.Attach
	if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		attach = func(socketPath string, in io.Reader, out io.Writer) error {
			return control.AttachInteractive(socketPath, os.Stdin, os.Stdout)
		}
	}
	if err := attach(socketPath, os.Stdin, os.Stdout); err != nil {
		if errors.Is(err, os.ErrNotExist) ||
			errors.Is(err, syscall.ECONNREFUSED) ||
			errors.Is(err, syscall.ENOTSOCK) ||
			errors.Is(err, syscall.EPROTOTYPE) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func fatalStartupError(err error, alsoPrint bool) {
	if alsoPrint {
		fmt.Fprintln(os.Stderr, err)
	}
	log.Fatal(err)
}

func warnLegacyProtectionSettings(cfg runtimeconfig.Config, alsoPrint bool) {
	message := legacyProtectionWarning(cfg)
	if message == "" {
		return
	}
	log.Print(message)
	if alsoPrint {
		fmt.Fprintln(os.Stderr, "Warning: "+message)
	}
}

func legacyProtectionWarning(cfg runtimeconfig.Config) string {
	if !cfg.HasLegacyProtectionSettings() {
		return ""
	}
	return "legacy [protection] config is ignored; configure [redis] and run /protection enable once in the console"
}

var runtimeConfigPath = runtimeconfig.DefaultConfigPath

func buildRuntimeConfig() (runtimeconfig.Config, error) {
	cfg, err := loadRuntimeConfig()
	if err != nil {
		return runtimeconfig.Config{}, err
	}
	return cfg.Resolve()
}

func loadRuntimeConfig() (runtimeconfig.Config, error) {
	cfg, err := runtimeconfig.LoadFile(runtimeConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return runtimeconfig.Default(), nil
		}
		return runtimeconfig.Config{}, err
	}
	return cfg, nil
}
