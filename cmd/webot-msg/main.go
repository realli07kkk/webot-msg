package main

import (
	"flag"
	"log"

	"github.com/realli07kkk/webot-msg/internal/app"
	"github.com/realli07kkk/webot-msg/internal/logfile"
	"github.com/realli07kkk/webot-msg/internal/runtimeconfig"
)

func main() {
	port := flag.Int("port", runtimeconfig.DefaultPort, "API server port")
	configPath := flag.String("c", "", "TOML config file path")
	flag.Parse()

	resolved, err := buildRuntimeConfig(*configPath, *port, flagIsSet("port"))
	if err != nil {
		log.Fatal(err)
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
		log.Printf("Runtime config loaded: api_port=%d auth_path=%s base_url=%s log_file=%s log_max_size_bytes=%d",
			resolved.API.Port, resolved.Storage.AuthPath, resolved.Ilink.BaseURL, resolved.Log.FilePath, resolved.Log.MaxSizeBytes)
		if legacyAuthCopied {
			log.Printf("Legacy auth store copied: source=%s target=%s", runtimeconfig.LegacyAuthPath, resolved.Storage.AuthPath)
		}
	}

	application := app.New(resolved.Storage.AuthPath, resolved.Ilink.BaseURL)
	if err := application.Run(resolved.API.Port); err != nil {
		log.Fatal(err)
	}
}

func buildRuntimeConfig(configPath string, port int, portSet bool) (runtimeconfig.Config, error) {
	cfg, err := loadRuntimeConfig(configPath)
	if err != nil {
		return runtimeconfig.Config{}, err
	}
	if portSet {
		cfg.API.Port = port
	}
	return cfg.Resolve()
}

func loadRuntimeConfig(path string) (runtimeconfig.Config, error) {
	if path == "" {
		return runtimeconfig.Default(), nil
	}
	return runtimeconfig.LoadFile(path)
}

func flagIsSet(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
