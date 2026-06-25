package protection

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RuntimeGuard struct {
	mu      sync.Mutex
	current *guardGeneration
	enabled bool
}

type guardGeneration struct {
	guard   Guard
	client  *redis.Client
	refs    int
	retired bool
}

type EnableConfig struct {
	RedisURL                string
	RedisPassword           string
	KeyPrefix               string
	MessageLimit            int
	MessageWarningRemaining int
	ActiveWindow            time.Duration
	TimeWarningBefore       time.Duration
	QueueMaxLen             int
	QueueTTL                time.Duration
}

func NewRuntimeGuard() *RuntimeGuard {
	return &RuntimeGuard{}
}

func (g *RuntimeGuard) Enabled() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.enabled
}

func (g *RuntimeGuard) Enable(ctx context.Context, cfg EnableConfig) error {
	client, err := NewRedisClient(cfg.RedisURL, cfg.RedisPassword)
	if err != nil {
		return err
	}
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return err
	}

	redisGuard := NewRedisGuard(client, RedisGuardConfig{
		KeyPrefix:               cfg.KeyPrefix,
		MessageLimit:            cfg.MessageLimit,
		MessageWarningRemaining: cfg.MessageWarningRemaining,
		ActiveWindow:            cfg.ActiveWindow,
		TimeWarningBefore:       cfg.TimeWarningBefore,
		QueueMaxLen:             cfg.QueueMaxLen,
		QueueTTL:                cfg.QueueTTL,
	})

	g.mu.Lock()
	oldGeneration := g.current
	g.current = &guardGeneration{guard: redisGuard, client: client}
	g.enabled = true
	oldClient := g.retireLocked(oldGeneration)
	g.mu.Unlock()

	closeRedisClient(oldClient)
	return nil
}

func (g *RuntimeGuard) Disable() {
	if g == nil {
		return
	}
	g.mu.Lock()
	oldGeneration := g.current
	g.current = nil
	g.enabled = false
	oldClient := g.retireLocked(oldGeneration)
	g.mu.Unlock()

	closeRedisClient(oldClient)
}

func (g *RuntimeGuard) BeginOperation() Operation {
	if g == nil {
		return staticOperation{Guard: NoopGuard{}}
	}

	g.mu.Lock()
	if !g.enabled || g.current == nil || g.current.guard == nil {
		g.mu.Unlock()
		return staticOperation{Guard: NoopGuard{}}
	}
	generation := g.current
	generation.refs++
	g.mu.Unlock()

	return &runtimeOperation{
		parent:     g,
		generation: generation,
		Guard:      generation.guard,
	}
}

func (g *RuntimeGuard) RuntimeStatus(ctx context.Context, botID string) (Status, error) {
	operation := g.BeginOperation()
	defer operation.Done()

	statusProvider, ok := operation.(interface {
		ProtectionStatus(context.Context, string) (Status, error)
	})
	if !ok {
		return Status{Enabled: false, BotID: botID}, nil
	}
	status, err := statusProvider.ProtectionStatus(ctx, botID)
	if err != nil {
		return Status{}, err
	}
	status.Enabled = true
	return status, nil
}

func (g *RuntimeGuard) ReserveNormalSend(ctx context.Context, botID string) (Reservation, error) {
	operation := g.BeginOperation()
	defer operation.Done()
	return operation.ReserveNormalSend(ctx, botID)
}

func (g *RuntimeGuard) ReleaseNormalSend(ctx context.Context, botID string) error {
	operation := g.BeginOperation()
	defer operation.Done()
	return operation.ReleaseNormalSend(ctx, botID)
}

func (g *RuntimeGuard) RecordReminderSend(ctx context.Context, botID string) error {
	operation := g.BeginOperation()
	defer operation.Done()
	return operation.RecordReminderSend(ctx, botID)
}

func (g *RuntimeGuard) RecordActiveConversation(ctx context.Context, botID string) error {
	operation := g.BeginOperation()
	defer operation.Done()
	return operation.RecordActiveConversation(ctx, botID)
}

func (g *RuntimeGuard) CheckTimeWindow(ctx context.Context, botID string) (Decision, error) {
	operation := g.BeginOperation()
	defer operation.Done()
	return operation.CheckTimeWindow(ctx, botID)
}

func (g *RuntimeGuard) retireLocked(generation *guardGeneration) *redis.Client {
	if generation == nil {
		return nil
	}
	generation.retired = true
	if generation.refs > 0 {
		return nil
	}
	client := generation.client
	generation.client = nil
	return client
}

func (g *RuntimeGuard) finishOperation(generation *guardGeneration) {
	if g == nil || generation == nil {
		return
	}

	g.mu.Lock()
	if generation.refs > 0 {
		generation.refs--
	}
	var client *redis.Client
	if generation.refs == 0 && generation.retired {
		client = generation.client
		generation.client = nil
	}
	g.mu.Unlock()

	closeRedisClient(client)
}

type runtimeOperation struct {
	parent     *RuntimeGuard
	generation *guardGeneration
	Guard
	done sync.Once
}

func (o *runtimeOperation) Done() {
	if o == nil {
		return
	}
	o.done.Do(func() {
		o.parent.finishOperation(o.generation)
	})
}

func (o *runtimeOperation) ProtectionStatus(ctx context.Context, botID string) (Status, error) {
	if o == nil || o.Guard == nil {
		return Status{BotID: botID}, nil
	}
	statusProvider, ok := o.Guard.(interface {
		ProtectionStatus(context.Context, string) (Status, error)
	})
	if !ok {
		return Status{BotID: botID}, nil
	}
	return statusProvider.ProtectionStatus(ctx, botID)
}

func (o *runtimeOperation) AcquireOrEnqueue(ctx context.Context, botID string, text string) (Ingress, error) {
	controller, ok := o.sendQueueController()
	if !ok {
		return Ingress{Outcome: IngressSendNow, Reservation: SendNormal()}, nil
	}
	return controller.AcquireOrEnqueue(ctx, botID, text)
}

func (o *runtimeOperation) PeekQueued(ctx context.Context, botID string) (string, int64, bool, error) {
	controller, ok := o.sendQueueController()
	if !ok {
		return "", 0, false, nil
	}
	return controller.PeekQueued(ctx, botID)
}

func (o *runtimeOperation) DropFront(ctx context.Context, botID string) error {
	controller, ok := o.sendQueueController()
	if !ok {
		return nil
	}
	return controller.DropFront(ctx, botID)
}

func (o *runtimeOperation) QueueLen(ctx context.Context, botID string) (int, error) {
	controller, ok := o.sendQueueController()
	if !ok {
		return 0, nil
	}
	return controller.QueueLen(ctx, botID)
}

func (o *runtimeOperation) sendQueueController() (SendQueueController, bool) {
	if o == nil || o.Guard == nil {
		return nil, false
	}
	controller, ok := o.Guard.(SendQueueController)
	return controller, ok
}

func closeRedisClient(client *redis.Client) {
	if client != nil {
		_ = client.Close()
	}
}
