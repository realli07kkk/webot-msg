package telemetry

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	otelsdk "go.opentelemetry.io/otel/sdk"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	defaultProtocol    = "grpc"
	defaultServiceName = "webot-msg"
)

var otlpExporterEnvMu sync.Mutex

type Config struct {
	Endpoint           string
	Protocol           string
	Insecure           bool
	ServiceName        string
	Headers            map[string]string
	ResourceAttributes map[string]string
}

func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	if cfg.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	exp, err := newExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(newResource(cfg)),
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tracerProvider.Shutdown, nil
}

func newExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	return withOTLPExporterEnvDisabled(func() (sdktrace.SpanExporter, error) {
		switch protocol := normalizedProtocol(cfg.Protocol); protocol {
		case "grpc":
			opts := []otlptracegrpc.Option{
				otlptracegrpc.WithEndpoint(cfg.Endpoint),
				otlptracegrpc.WithHeaders(copyMap(cfg.Headers)),
			}
			if cfg.Insecure {
				opts = append(opts, otlptracegrpc.WithInsecure())
			}
			return otlptracegrpc.New(ctx, opts...)
		case "http":
			opts := []otlptracehttp.Option{
				otlptracehttp.WithEndpoint(cfg.Endpoint),
				otlptracehttp.WithHeaders(copyMap(cfg.Headers)),
			}
			if cfg.Insecure {
				opts = append(opts, otlptracehttp.WithInsecure())
			}
			return otlptracehttp.New(ctx, opts...)
		default:
			return nil, fmt.Errorf("telemetry.protocol: must be grpc or http")
		}
	})
}

func withOTLPExporterEnvDisabled(build func() (sdktrace.SpanExporter, error)) (exp sdktrace.SpanExporter, err error) {
	otlpExporterEnvMu.Lock()
	defer otlpExporterEnvMu.Unlock()

	saved := map[string]string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !isOTLPExporterEnv(key) {
			continue
		}
		saved[key] = os.Getenv(key)
		if unsetErr := os.Unsetenv(key); unsetErr != nil {
			return nil, fmt.Errorf("clear %s: %w", key, unsetErr)
		}
	}

	defer func() {
		for key, value := range saved {
			if restoreErr := os.Setenv(key, value); restoreErr != nil && err == nil {
				err = fmt.Errorf("restore %s: %w", key, restoreErr)
			}
		}
	}()

	return build()
}

func isOTLPExporterEnv(key string) bool {
	return key == "OTEL_EXPORTER_OTLP" || strings.HasPrefix(key, "OTEL_EXPORTER_OTLP_")
}

func newResource(cfg Config) *resource.Resource {
	detected, err := resource.New(
		context.Background(),
		resource.WithHost(),
		resource.WithTelemetrySDK(),
		resource.WithService(),
	)
	if err != nil && detected == nil {
		detected = resource.Empty()
	}

	attrs := []attribute.KeyValue{
		attribute.String("service.name", serviceName(cfg.ServiceName)),
		attribute.String("agent.version", otelsdk.Version()),
	}

	keys := make([]string, 0, len(cfg.ResourceAttributes))
	for key := range cfg.ResourceAttributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		attrs = append(attrs, attribute.String(key, cfg.ResourceAttributes[key]))
	}

	configured := resource.NewWithAttributes("", attrs...)
	merged, err := resource.Merge(detected, configured)
	if err != nil {
		return configured
	}
	return merged
}

func normalizedProtocol(protocol string) string {
	protocol = strings.TrimSpace(protocol)
	if protocol == "" {
		return defaultProtocol
	}
	return protocol
}

func serviceName(service string) string {
	service = strings.TrimSpace(service)
	if service == "" {
		return defaultServiceName
	}
	return service
}

func copyMap(values map[string]string) map[string]string {
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}
