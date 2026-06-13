package audit

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/realli07kkk/webot-msg/internal/protection"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type Auditor interface {
	Enabled() bool
	Record(ctx context.Context, in RecordInput) error
}

type RecordInput struct {
	ID     string
	SentAt time.Time
	Body   string
}

type EnableConfig struct {
	RedisURL      string
	RedisPassword string
	KeyPrefix     string
	TimeTTL       time.Duration
	BodyTTL       time.Duration
}

type NoopAuditor struct{}

func (NoopAuditor) Enabled() bool {
	return false
}

func (NoopAuditor) Record(context.Context, RecordInput) error {
	return nil
}

type Recorder struct {
	mu     sync.RWMutex
	client *redis.Client
	cfg    EnableConfig
}

func NewRecorder() *Recorder {
	return &Recorder{}
}

func (r *Recorder) Enable(ctx context.Context, cfg EnableConfig) error {
	if r == nil {
		return fmt.Errorf("runtime audit recorder is not available")
	}
	if cfg.TimeTTL <= 0 {
		return fmt.Errorf("audit.time_ttl: must be positive")
	}
	if cfg.BodyTTL <= 0 {
		return fmt.Errorf("audit.body_ttl: must be positive")
	}
	cfg.KeyPrefix = strings.TrimSpace(cfg.KeyPrefix)
	if cfg.KeyPrefix == "" {
		return fmt.Errorf("audit key prefix: must not be empty")
	}

	client, err := protection.NewRedisClient(cfg.RedisURL, cfg.RedisPassword)
	if err != nil {
		return err
	}
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return err
	}

	r.mu.Lock()
	oldClient := r.client
	r.client = client
	r.cfg = cfg
	r.mu.Unlock()

	if oldClient != nil {
		_ = oldClient.Close()
	}
	return nil
}

func (r *Recorder) Disable() {
	if r == nil {
		return
	}
	r.mu.Lock()
	client := r.client
	r.client = nil
	r.cfg = EnableConfig{}
	r.mu.Unlock()

	if client != nil {
		_ = client.Close()
	}
}

func (r *Recorder) Enabled() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.client != nil
}

func (r *Recorder) Record(ctx context.Context, in RecordInput) error {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.client == nil {
		return nil
	}

	recordCtx, span := startRecordSpan(ctx, in.ID)
	if span != nil {
		defer span.End()
	}

	_, err := r.client.Pipelined(recordCtx, func(pipe redis.Pipeliner) error {
		pipe.Set(recordCtx, r.timeKey(in.ID), strconv.FormatInt(in.SentAt.UnixMilli(), 10), r.cfg.TimeTTL)
		pipe.Set(recordCtx, r.bodyKey(in.ID), in.Body, r.cfg.BodyTTL)
		return nil
	})
	if err != nil && span != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "audit redis write failed")
	}
	return err
}

func (r *Recorder) TimeKey(id string) string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return auditTimeKey(r.cfg.KeyPrefix, id)
}

func (r *Recorder) BodyKey(id string) string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return auditBodyKey(r.cfg.KeyPrefix, id)
}

func (r *Recorder) timeKey(id string) string {
	return auditTimeKey(r.cfg.KeyPrefix, id)
}

func (r *Recorder) bodyKey(id string) string {
	return auditBodyKey(r.cfg.KeyPrefix, id)
}

func auditTimeKey(prefix string, id string) string {
	return prefix + ":audit:time:" + id
}

func auditBodyKey(prefix string, id string) string {
	return prefix + ":audit:body:" + id
}

func startRecordSpan(ctx context.Context, id string) (context.Context, oteltrace.Span) {
	if !oteltrace.SpanContextFromContext(ctx).IsValid() {
		return ctx, nil
	}
	return otel.Tracer("github.com/realli07kkk/webot-msg/internal/audit").Start(
		ctx,
		"audit.record",
		oteltrace.WithAttributes(
			attribute.String("db.system.name", "redis"),
			attribute.String("db.operation.name", "set"),
			attribute.String("audit.message_id", id),
		),
	)
}
