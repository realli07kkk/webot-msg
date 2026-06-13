package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	otelsdk "go.opentelemetry.io/otel/sdk"
	oteltrace "go.opentelemetry.io/otel/trace"
	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestSetupDisabledReturnsNoopShutdown(t *testing.T) {
	resetTracerProvider(t)

	shutdown, err := Setup(context.Background(), Config{
		Protocol: "tcp",
	})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if shutdown == nil {
		t.Fatal("Setup() shutdown = nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}
}

func TestSetupRejectsInvalidProtocol(t *testing.T) {
	resetTracerProvider(t)

	shutdown, err := Setup(context.Background(), Config{
		Endpoint: "localhost:4317",
		Protocol: "tcp",
	})
	if err == nil {
		t.Fatal("Setup() error = nil")
	}
	if shutdown != nil {
		t.Fatal("Setup() shutdown is not nil")
	}
	if !strings.Contains(err.Error(), "telemetry.protocol") {
		t.Fatalf("Setup() error = %q, want telemetry.protocol", err.Error())
	}
}

func TestSetupHTTPExporterSendsHeadersAndResourceAttributes(t *testing.T) {
	resetTracerProvider(t)

	requests := make(chan *collectortracepb.ExportTraceServiceRequest, 1)
	errors := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			errors <- "unexpected path: " + r.URL.Path
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			errors <- "unexpected Authorization header: " + got
			http.Error(w, "bad header", http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			errors <- "read body: " + err.Error()
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		var exportReq collectortracepb.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &exportReq); err != nil {
			errors <- "unmarshal body: " + err.Error()
			http.Error(w, "bad protobuf", http.StatusBadRequest)
			return
		}
		requests <- &exportReq
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	shutdown, err := Setup(context.Background(), Config{
		Endpoint:    endpointHost(t, server.URL),
		Protocol:    "http",
		Insecure:    true,
		ServiceName: "custom-service",
		Headers: map[string]string{
			"Authorization": "Bearer secret",
		},
		ResourceAttributes: map[string]string{
			"token": "tencent-token",
			"env":   "test",
		},
	})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	_, span := otel.Tracer("telemetry-test").Start(context.Background(), "test-span")
	span.End()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}

	select {
	case errText := <-errors:
		t.Fatal(errText)
	case exportReq := <-requests:
		hostname, err := os.Hostname()
		if err != nil {
			t.Fatalf("hostname: %v", err)
		}

		assertResourceAttribute(t, exportReq, "service.name", "custom-service")
		assertResourceAttribute(t, exportReq, "host.name", hostname)
		assertResourceAttribute(t, exportReq, "agent.version", otelsdk.Version())
		assertResourceAttribute(t, exportReq, "telemetry.sdk.name", "opentelemetry")
		assertResourceAttribute(t, exportReq, "telemetry.sdk.language", "go")
		assertResourceAttribute(t, exportReq, "telemetry.sdk.version", otelsdk.Version())
		assertResourceAttributeNonEmpty(t, exportReq, "service.instance.id")
		assertResourceAttribute(t, exportReq, "token", "tencent-token")
		assertResourceAttribute(t, exportReq, "env", "test")
	default:
		t.Fatal("collector did not receive export request")
	}
}

func TestSetupIgnoresAmbientOTLPExporterEnv(t *testing.T) {
	resetTracerProvider(t)

	envValues := map[string]string{
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT":           "http://env.example/custom",
		"OTEL_EXPORTER_OTLP_HEADERS":                   "X-Env=bad",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS":            "X-Trace-Env=bad",
		"OTEL_EXPORTER_OTLP_COMPRESSION":               "gzip",
		"OTEL_EXPORTER_OTLP_TRACES_TIMEOUT":            "1",
		"OTEL_EXPORTER_OTLP_TRACES_CERTIFICATE":        "/path/does/not/exist",
		"OTEL_EXPORTER_OTLP_TRACES_CLIENT_CERTIFICATE": "/path/does/not/exist",
		"OTEL_EXPORTER_OTLP_TRACES_CLIENT_KEY":         "/path/does/not/exist",
		"OTEL_EXPORTER_OTLP_TRACES_INSECURE":           "false",
	}
	for key, value := range envValues {
		t.Setenv(key, value)
	}

	requests := make(chan *collectortracepb.ExportTraceServiceRequest, 1)
	errors := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			errors <- "unexpected path: " + r.URL.Path
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("X-Env"); got != "" {
			errors <- "unexpected X-Env header: " + got
			http.Error(w, "bad header", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("X-Trace-Env"); got != "" {
			errors <- "unexpected X-Trace-Env header: " + got
			http.Error(w, "bad header", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Content-Encoding"); got == "gzip" {
			errors <- "unexpected gzip compression"
			http.Error(w, "bad compression", http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			errors <- "read body: " + err.Error()
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		var exportReq collectortracepb.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &exportReq); err != nil {
			errors <- "unmarshal body: " + err.Error()
			http.Error(w, "bad protobuf", http.StatusBadRequest)
			return
		}
		requests <- &exportReq
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	shutdown, err := Setup(context.Background(), Config{
		Endpoint:    endpointHost(t, server.URL),
		Protocol:    "http",
		Insecure:    true,
		ServiceName: "custom-service",
	})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	for key, want := range envValues {
		if got := os.Getenv(key); got != want {
			t.Fatalf("%s after Setup = %q, want restored %q", key, got, want)
		}
	}

	_, span := otel.Tracer("telemetry-test").Start(context.Background(), "test-span")
	span.End()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}

	select {
	case errText := <-errors:
		t.Fatal(errText)
	case exportReq := <-requests:
		assertResourceAttribute(t, exportReq, "service.name", "custom-service")
	default:
		t.Fatal("collector did not receive export request")
	}
}

func TestSetupGRPCExporterInitializes(t *testing.T) {
	resetTracerProvider(t)

	shutdown, err := Setup(context.Background(), Config{
		Endpoint: "localhost:4317",
		Protocol: "grpc",
		Insecure: true,
	})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if shutdown == nil {
		t.Fatal("Setup() shutdown = nil")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}
}

func endpointHost(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	return parsed.Host
}

func assertResourceAttribute(t *testing.T, exportReq *collectortracepb.ExportTraceServiceRequest, key string, want string) {
	t.Helper()
	for _, resourceSpan := range exportReq.ResourceSpans {
		for _, attr := range resourceSpan.Resource.Attributes {
			if attr.Key == key {
				if got := attr.Value.GetStringValue(); got != want {
					t.Fatalf("resource attribute %s = %q, want %q", key, got, want)
				}
				return
			}
		}
	}
	t.Fatalf("resource attribute %s not found", key)
}

func assertResourceAttributeNonEmpty(t *testing.T, exportReq *collectortracepb.ExportTraceServiceRequest, key string) {
	t.Helper()
	for _, resourceSpan := range exportReq.ResourceSpans {
		for _, attr := range resourceSpan.Resource.Attributes {
			if attr.Key == key {
				if got := attr.Value.GetStringValue(); got == "" {
					t.Fatalf("resource attribute %s is empty", key)
				}
				return
			}
		}
	}
	t.Fatalf("resource attribute %s not found", key)
}

func resetTracerProvider(t *testing.T) {
	t.Helper()
	otel.SetTracerProvider(oteltrace.NewNoopTracerProvider())
	t.Cleanup(func() {
		otel.SetTracerProvider(oteltrace.NewNoopTracerProvider())
	})
}
