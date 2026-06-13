package api

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/realli07kkk/webot-msg/internal/audit"
	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/ilink"
	"github.com/realli07kkk/webot-msg/internal/protection"
	"github.com/realli07kkk/webot-msg/internal/sender"
	"github.com/realli07kkk/webot-msg/internal/telemetry"
)

func TestTelemetryE2EExportsInboundOutboundAndAuditSpansWithSameTrace(t *testing.T) {
	resetOpenTelemetry(t)

	collector := newTraceCollector(t)
	shutdown, err := telemetry.Setup(context.Background(), telemetry.Config{
		Endpoint:    testEndpoint(t, collector.URL),
		Protocol:    "http",
		Insecure:    true,
		ServiceName: "webot-msg-test",
	})
	if err != nil {
		t.Fatalf("telemetry.Setup() error = %v", err)
	}

	ilinkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/sendmessage" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("traceparent"); got == "" {
			http.Error(w, "missing traceparent", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ret":0,"errcode":0}`)
	}))
	defer ilinkServer.Close()

	store := newAPIStore(t)
	client := ilink.NewClient(ilinkServer.URL)
	auditor := newEnabledTestAuditor(t)
	server := NewServerWithClientOptions(store, client, protection.NoopGuard{}, "reminder", sender.TextOptions{
		IDGenerator: func() (string, error) {
			return fixedMessageID, nil
		},
		Auditor: auditor,
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bots/bot-1/messages?token=api-token&text=hello", nil)
	server.handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Fatalf("telemetry shutdown error = %v", err)
	}

	spans := collector.SpansAtLeast(t, 3)
	serverTraceID := traceIDForKind(spans, tracepb.Span_SPAN_KIND_SERVER)
	clientTraceID := traceIDForKind(spans, tracepb.Span_SPAN_KIND_CLIENT)
	auditTraceID := traceIDForName(spans, "audit.record")
	if serverTraceID == "" {
		t.Fatalf("server span not found; spans=%d", len(spans))
	}
	if clientTraceID == "" {
		t.Fatalf("client span not found; spans=%d", len(spans))
	}
	if auditTraceID == "" {
		t.Fatalf("audit.record span not found; spans=%d", len(spans))
	}
	if serverTraceID != clientTraceID || serverTraceID != auditTraceID {
		t.Fatalf("trace_id mismatch: server=%s client=%s audit=%s", serverTraceID, clientTraceID, auditTraceID)
	}
}

func TestTelemetryDisabledDoesNotPropagateTraceparent(t *testing.T) {
	resetOpenTelemetry(t)

	ilinkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("traceparent"); got != "" {
			http.Error(w, "unexpected traceparent", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ret":0,"errcode":0}`)
	}))
	defer ilinkServer.Close()

	server := NewServer(newAPIStore(t), ilink.NewClient(ilinkServer.URL), protection.NoopGuard{}, "reminder")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bots/bot-1/messages?token=api-token&text=hello", nil)
	server.handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
}

func TestTelemetryExportFailureDoesNotBlockAPIResponse(t *testing.T) {
	resetOpenTelemetry(t)

	shutdown, err := telemetry.Setup(context.Background(), telemetry.Config{
		Endpoint:    unusedLocalEndpoint(t),
		Protocol:    "http",
		Insecure:    true,
		ServiceName: "webot-msg-test",
	})
	if err != nil {
		t.Fatalf("telemetry.Setup() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_ = shutdown(ctx)
	})

	ilinkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ret":0,"errcode":0}`)
	}))
	defer ilinkServer.Close()

	server := NewServer(newAPIStore(t), ilink.NewClient(ilinkServer.URL), protection.NoopGuard{}, "reminder")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bots/bot-1/messages?token=api-token&text=hello", nil)
	server.handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
}

func TestTelemetrySpansOmitSensitiveRequestValues(t *testing.T) {
	resetOpenTelemetry(t)

	collector := newTraceCollector(t)
	shutdown, err := telemetry.Setup(context.Background(), telemetry.Config{
		Endpoint:    testEndpoint(t, collector.URL),
		Protocol:    "http",
		Insecure:    true,
		ServiceName: "webot-msg-test",
	})
	if err != nil {
		t.Fatalf("telemetry.Setup() error = %v", err)
	}

	ilinkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ret":0,"errcode":0}`)
	}))
	defer ilinkServer.Close()

	store := newAPIStore(t)
	if _, err := store.UpdateBot("bot-1", func(user *config.UserConfig) bool {
		user.BotToken = "bot-token-secret"
		user.ContextToken = "ctx-token-secret"
		user.APIToken = "api-token-secret"
		return true
	}); err != nil {
		t.Fatalf("UpdateBot() error = %v", err)
	}

	server := NewServer(store, ilink.NewClient(ilinkServer.URL), protection.NoopGuard{}, "reminder")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bots/bot-1/messages?token=api-token-secret&text=message-secret", nil)
	server.handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Fatalf("telemetry shutdown error = %v", err)
	}

	spans := collector.Spans(t)
	allSpans := fmt.Sprint(spans)
	for _, secret := range []string{"api-token-secret", "message-secret", "bot-token-secret", "ctx-token-secret"} {
		if strings.Contains(allSpans, secret) {
			t.Fatalf("exported spans contain sensitive value %q: %s", secret, allSpans)
		}
	}
}

type traceCollector struct {
	*httptest.Server
	requests chan *collectortracepb.ExportTraceServiceRequest
	errors   chan string
}

func newTraceCollector(t *testing.T) *traceCollector {
	t.Helper()
	collector := &traceCollector{
		requests: make(chan *collectortracepb.ExportTraceServiceRequest, 4),
		errors:   make(chan string, 1),
	}
	collector.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			collector.errors <- "unexpected collector path: " + r.URL.Path
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			collector.errors <- "read collector body: " + err.Error()
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		var exportReq collectortracepb.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &exportReq); err != nil {
			collector.errors <- "unmarshal collector body: " + err.Error()
			http.Error(w, "bad protobuf", http.StatusBadRequest)
			return
		}
		collector.requests <- &exportReq
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(collector.Close)
	return collector
}

func (c *traceCollector) Spans(t *testing.T) []*tracepb.Span {
	return c.SpansAtLeast(t, 2)
}

func (c *traceCollector) SpansAtLeast(t *testing.T, want int) []*tracepb.Span {
	t.Helper()

	var spans []*tracepb.Span
	timeout := time.After(2 * time.Second)
	for len(spans) < want {
		select {
		case errText := <-c.errors:
			t.Fatal(errText)
		case exportReq := <-c.requests:
			for _, resourceSpans := range exportReq.ResourceSpans {
				for _, scopeSpans := range resourceSpans.ScopeSpans {
					spans = append(spans, scopeSpans.Spans...)
				}
			}
		case <-timeout:
			t.Fatalf("timed out waiting for exported spans; got %d, want at least %d", len(spans), want)
		}
	}
	return spans
}

func traceIDForKind(spans []*tracepb.Span, kind tracepb.Span_SpanKind) string {
	for _, span := range spans {
		if span.Kind == kind {
			return hex.EncodeToString(span.TraceId)
		}
	}
	return ""
}

func traceIDForName(spans []*tracepb.Span, name string) string {
	for _, span := range spans {
		if span.Name == name {
			return hex.EncodeToString(span.TraceId)
		}
	}
	return ""
}

func newEnabledTestAuditor(t *testing.T) *audit.Recorder {
	t.Helper()
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	t.Cleanup(redisServer.Close)

	auditor := audit.NewRecorder()
	if err := auditor.Enable(context.Background(), audit.EnableConfig{
		RedisURL:  "redis://" + redisServer.Addr() + "/0",
		KeyPrefix: "webot-msg",
		TimeTTL:   time.Hour,
		BodyTTL:   time.Hour,
	}); err != nil {
		t.Fatalf("Enable audit recorder error = %v", err)
	}
	t.Cleanup(auditor.Disable)
	return auditor
}

func testEndpoint(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	return parsed.Host
}

func unusedLocalEndpoint(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen unused endpoint: %v", err)
	}
	endpoint := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close unused endpoint listener: %v", err)
	}
	return endpoint
}

func resetOpenTelemetry(t *testing.T) {
	t.Helper()
	otel.SetTracerProvider(oteltrace.NewNoopTracerProvider())
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
	t.Cleanup(func() {
		otel.SetTracerProvider(oteltrace.NewNoopTracerProvider())
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
	})
}
