package audit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const testMessageID = "01890f3e-6f44-7b2c-8d9e-123456789abc"

func TestRecorderRecordWritesAuditKeysWithTTL(t *testing.T) {
	recorder, redisServer := newTestRecorder(t, EnableConfig{
		KeyPrefix: "webot-msg",
		TimeTTL:   2 * time.Hour,
		BodyTTL:   30 * time.Minute,
	})

	sentAt := time.Unix(1700000000, 123000000)
	body := "hello\n" + testMessageID
	if err := recorder.Record(context.Background(), RecordInput{
		ID:     testMessageID,
		SentAt: sentAt,
		Body:   body,
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	timeKey := "webot-msg:audit:time:" + testMessageID
	bodyKey := "webot-msg:audit:body:" + testMessageID
	gotTime, err := redisServer.Get(timeKey)
	if err != nil {
		t.Fatalf("time key missing: %v", err)
	}
	if gotTime != "1700000000123" {
		t.Fatalf("time key value = %q, want unix millis", gotTime)
	}
	gotBody, err := redisServer.Get(bodyKey)
	if err != nil {
		t.Fatalf("body key missing: %v", err)
	}
	if gotBody != body {
		t.Fatalf("body key value = %q, want final body", gotBody)
	}
	if ttl := redisServer.TTL(timeKey); ttl != 2*time.Hour {
		t.Fatalf("time key TTL = %s, want 2h", ttl)
	}
	if ttl := redisServer.TTL(bodyKey); ttl != 30*time.Minute {
		t.Fatalf("body key TTL = %s, want 30m", ttl)
	}
}

func TestRecorderRecordClosedNoop(t *testing.T) {
	recorder := NewRecorder()

	if err := recorder.Record(context.Background(), RecordInput{
		ID:     testMessageID,
		SentAt: time.Now(),
		Body:   "hello",
	}); err != nil {
		t.Fatalf("Record() error = %v, want nil when disabled", err)
	}
	if recorder.Enabled() {
		t.Fatal("Enabled() = true, want false")
	}
}

func TestRecorderRecordReturnsRedisWriteError(t *testing.T) {
	recorder, redisServer := newTestRecorder(t, EnableConfig{
		KeyPrefix: "webot-msg",
		TimeTTL:   time.Hour,
		BodyTTL:   time.Hour,
	})
	redisServer.Close()

	err := recorder.Record(context.Background(), RecordInput{
		ID:     testMessageID,
		SentAt: time.Now(),
		Body:   "hello",
	})
	if err == nil {
		t.Fatal("Record() error = nil, want redis write error")
	}
}

func TestRecorderRecordCreatesSpanOnlyWithParent(t *testing.T) {
	recorder, _ := newTestRecorder(t, EnableConfig{
		KeyPrefix: "webot-msg",
		TimeTTL:   time.Hour,
		BodyTTL:   time.Hour,
	})
	spanRecorder, tracerProvider := installTestTracer(t)

	if err := recorder.Record(context.Background(), RecordInput{
		ID:     testMessageID,
		SentAt: time.Now(),
		Body:   "hello",
	}); err != nil {
		t.Fatalf("Record() without parent error = %v", err)
	}
	if got := len(spanRecorder.Ended()); got != 0 {
		t.Fatalf("ended spans without parent = %d, want 0", got)
	}

	ctx, parent := tracerProvider.Tracer("test").Start(context.Background(), "parent")
	if err := recorder.Record(ctx, RecordInput{
		ID:     testMessageID,
		SentAt: time.Now(),
		Body:   "hello",
	}); err != nil {
		t.Fatalf("Record() with parent error = %v", err)
	}
	parent.End()

	spans := spanRecorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("ended spans = %d, want audit child + parent", len(spans))
	}
	var auditSpan sdktrace.ReadOnlySpan
	for _, span := range spans {
		if span.Name() == "audit.record" {
			auditSpan = span
			break
		}
	}
	if auditSpan == nil {
		t.Fatalf("spans = %#v, want audit.record span", spans)
	}
	if got := auditSpan.Parent().TraceID(); got != spans[1].SpanContext().TraceID() && got != spans[0].SpanContext().TraceID() {
		t.Fatalf("audit parent trace id = %s, want same trace as parent", got)
	}
	for _, attr := range auditSpan.Attributes() {
		switch attr.Key {
		case "audit.message_id":
			if attr.Value.AsString() != testMessageID {
				t.Fatalf("audit.message_id = %q, want %q", attr.Value.AsString(), testMessageID)
			}
		case "messaging.body", "redis.password":
			t.Fatalf("audit span contains forbidden attribute %s", attr.Key)
		}
	}
}

func newTestRecorder(t *testing.T, cfg EnableConfig) (*Recorder, *miniredis.Miniredis) {
	t.Helper()

	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	t.Cleanup(redisServer.Close)

	recorder := NewRecorder()
	cfg.RedisURL = "redis://" + redisServer.Addr() + "/0"
	if err := recorder.Enable(context.Background(), cfg); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	t.Cleanup(recorder.Disable)
	return recorder, redisServer
}

func installTestTracer(t *testing.T) (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	t.Helper()

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tracerProvider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = tracerProvider.Shutdown(context.Background())
		otel.SetTracerProvider(oteltrace.NewNoopTracerProvider())
	})
	return spanRecorder, tracerProvider
}
