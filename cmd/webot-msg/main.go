package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/realli07kkk/webot-msg/internal/app"
	"github.com/realli07kkk/webot-msg/internal/control"
	"github.com/realli07kkk/webot-msg/internal/logfile"
	"github.com/realli07kkk/webot-msg/internal/runtimeconfig"
)

func main() {
	opts, err := parseCLI(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	resolved, err := buildRuntimeConfig(opts.configPath, opts.configSet, opts.port, opts.portSet)
	if err != nil {
		log.Fatal(err)
	}

	if opts.command == "console" {
		if err := control.Attach(resolved.Control.SocketPath, os.Stdin, os.Stdout); err != nil {
			log.Fatal(err)
		}
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

	application := app.New(resolved.Storage.AuthPath, resolved.Ilink.BaseURL, resolved.Control.SocketPath)
	if err := application.Run(resolved.API.Port); err != nil {
		log.Fatal(err)
	}
}

type cliOptions struct {
	command    string
	configPath string
	configSet  bool
	port       int
	portSet    bool
}

var runtimeConfigPath = runtimeconfig.DefaultConfigPath

func parseCLI(args []string) (cliOptions, error) {
	opts := cliOptions{
		command: "serve",
		port:    runtimeconfig.DefaultPort,
	}

	flagArgs := args
	if len(args) > 0 && isCommand(args[0]) {
		opts.command = args[0]
		flagArgs = args[1:]
	}

	fs := flag.NewFlagSet("webot-msg", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("c", "", "TOML config file path (deprecated; default is ~/.webot-msg/config/webot-msg.toml)")
	port := fs.Int("port", runtimeconfig.DefaultPort, "API server port")
	if err := fs.Parse(flagArgs); err != nil {
		return cliOptions{}, err
	}

	if fs.NArg() > 0 {
		if opts.command != "serve" {
			return cliOptions{}, fmt.Errorf("unexpected argument(s): %s", strings.Join(fs.Args(), " "))
		}
		if fs.NArg() > 1 || !isCommand(fs.Arg(0)) {
			return cliOptions{}, fmt.Errorf("unknown command or argument: %s", strings.Join(fs.Args(), " "))
		}
		opts.command = fs.Arg(0)
	}

	opts.configPath = *configPath
	opts.port = *port
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "c" {
			opts.configSet = true
		}
		if f.Name == "port" {
			opts.portSet = true
		}
	})
	return opts, nil
}

func isCommand(value string) bool {
	return value == "serve" || value == "console"
}

func buildRuntimeConfig(configPath string, configSet bool, port int, portSet bool) (runtimeconfig.Config, error) {
	cfg, err := loadRuntimeConfig(configPath, configSet)
	if err != nil {
		return runtimeconfig.Config{}, err
	}
	if portSet {
		cfg.API.Port = port
	}
	return cfg.Resolve()
}

func loadRuntimeConfig(configPath string, configSet bool) (runtimeconfig.Config, error) {
	if configSet {
		return runtimeconfig.LoadFile(configPath)
	}

	cfg, err := runtimeconfig.LoadFile(runtimeConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return runtimeconfig.Default(), nil
		}
		return runtimeconfig.Config{}, err
	}
	return cfg, nil
}
