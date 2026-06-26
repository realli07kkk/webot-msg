package app

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/realli07kkk/webot-msg/internal/protection"
	"github.com/realli07kkk/webot-msg/internal/sender"
)

const sendQueueCommitTimeout = 5 * time.Second

const (
	// backlogReplayPrefix 标记一条消息是保护恢复后从积压队列补发的。
	backlogReplayPrefix     = "[积压补发]"
	backlogReplayTimeLayout = "2006-01-02 15:04:05 -0700"
)

var backlogReplayLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return loc
}()

// buildBacklogReplayNotice 生成积压补发消息的前缀，注明原始入队时间并说明因触发
// 发送保护被延迟补发。enqueuedMs 为收到 API 调用但触发保护无法发送的 Unix 毫秒时间。
func buildBacklogReplayNotice(enqueuedMs int64) string {
	if enqueuedMs <= 0 {
		return backlogReplayPrefix + " 收到 API 调用时间未知；该消息因发送保护限制进入积压队列，恢复后延迟补发。"
	}
	original := time.UnixMilli(enqueuedMs).In(backlogReplayLocation).Format(backlogReplayTimeLayout)
	return fmt.Sprintf("%s 收到 API 调用时间：%s；该消息因发送保护限制进入积压队列，恢复后延迟补发。", backlogReplayPrefix, original)
}

type sendQueueDrainer struct {
	ctx    context.Context
	cancel context.CancelFunc
	rerun  bool
}

func (a *App) startSendQueueDrainer(botID string) {
	if !a.protectionIsEnabled() {
		return
	}

	a.monitorMu.Lock()
	if a.runningSendQueueDrainers == nil {
		a.runningSendQueueDrainers = make(map[string]*sendQueueDrainer)
	}
	if drainer := a.runningSendQueueDrainers[botID]; drainer != nil {
		drainer.rerun = true
		a.monitorMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	drainer := &sendQueueDrainer{ctx: ctx, cancel: cancel}
	a.runningSendQueueDrainers[botID] = drainer
	a.monitorMu.Unlock()

	go a.monitorSendQueue(botID, drainer)
}

func (a *App) monitorSendQueue(botID string, drainer *sendQueueDrainer) {
	for {
		a.drainSendQueue(drainer.ctx, botID)

		a.monitorMu.Lock()
		current := a.runningSendQueueDrainers[botID]
		if current != drainer {
			a.monitorMu.Unlock()
			return
		}
		if drainer.rerun {
			drainer.rerun = false
			a.monitorMu.Unlock()
			continue
		}
		delete(a.runningSendQueueDrainers, botID)
		a.monitorMu.Unlock()
		return
	}
}

func (a *App) stopSendQueueDrainer(botID string) {
	a.monitorMu.Lock()
	drainer := a.runningSendQueueDrainers[botID]
	delete(a.runningSendQueueDrainers, botID)
	a.monitorMu.Unlock()
	if drainer != nil && drainer.cancel != nil {
		drainer.cancel()
	}
}

func (a *App) stopSendQueueDrainers() {
	a.monitorMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(a.runningSendQueueDrainers))
	for botID, drainer := range a.runningSendQueueDrainers {
		if drainer != nil && drainer.cancel != nil {
			cancels = append(cancels, drainer.cancel)
		}
		delete(a.runningSendQueueDrainers, botID)
	}
	a.monitorMu.Unlock()

	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
}

func (a *App) drainSendQueue(ctx context.Context, botID string) {
	guard := a.protectionGuard()
	operation := protection.BeginOperation(guard)
	defer operation.Done()

	controller, ok := appSendQueueController(operation, guard)
	if !ok {
		return
	}

	for {
		if ctx.Err() != nil {
			return
		}
		text, enqueuedMs, ok, err := controller.PeekQueued(ctx, botID)
		if err != nil {
			log.Printf("[Bot: %s] Send queue peek failed: %v", botID, err)
			return
		}
		if !ok {
			return
		}
		if sendQueuePayloadExpired(time.Now(), enqueuedMs, a.sendQueueTTL()) {
			if err := controller.DropFront(ctx, botID); err != nil {
				log.Printf("[Bot: %s] Send queue drop expired message failed: %v", botID, err)
				return
			}
			log.Printf("[Bot: %s] Send queue dropped expired message", botID)
			continue
		}

		user, exists := a.store.GetBot(botID)
		if !exists {
			log.Printf("[Bot: %s] Send queue drain stopped: bot not found", botID)
			return
		}
		if user.IlinkUserID == "" || user.ContextToken == "" {
			log.Printf("[Bot: %s] Send queue drain stopped: context not ready", botID)
			return
		}
		if ctx.Err() != nil {
			return
		}

		replayText := buildBacklogReplayNotice(enqueuedMs) + "\n" + text
		result, err := sender.SendProtectedTextWithOptions(ctx, a.client, operation, user, replayText, a.reminderText, sender.TextOptions{
			IDGenerator: a.idGenerator,
			Auditor:     a.auditor,
		})
		if err != nil {
			if protection.IsRejection(err) {
				log.Printf("[Bot: %s] Send queue drain stopped by protection: %s", botID, protection.RejectionReason(err))
			} else {
				log.Printf("[Bot: %s] Send queue drain stopped by send error: %v", botID, err)
			}
			return
		}
		if !result.NormalSent {
			log.Printf("[Bot: %s] Send queue drain stopped: queued message was not sent", botID)
			return
		}
		if err := dropSentQueueFront(controller, botID); err != nil {
			log.Printf("[Bot: %s] Send queue pop after send failed: %v", botID, err)
			return
		}
		log.Printf("[Bot: %s] Send queue replayed one message", botID)
	}
}

func dropSentQueueFront(controller protection.SendQueueController, botID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), sendQueueCommitTimeout)
	defer cancel()
	return controller.DropFront(ctx, botID)
}

func appSendQueueController(operation protection.Operation, guard protection.Guard) (protection.SendQueueController, bool) {
	if controller, ok := operation.(protection.SendQueueController); ok {
		return controller, true
	}
	controller, ok := guard.(protection.SendQueueController)
	return controller, ok
}

func (a *App) sendQueueTTL() time.Duration {
	if a.protectionConfig.QueueTTL > 0 {
		return a.protectionConfig.QueueTTL
	}
	if a.protectionConfig.ActiveWindow > 0 {
		return a.protectionConfig.ActiveWindow
	}
	return 24 * time.Hour
}

func sendQueuePayloadExpired(now time.Time, enqueuedMs int64, ttl time.Duration) bool {
	if ttl <= 0 || enqueuedMs <= 0 {
		return false
	}
	enqueuedAt := time.UnixMilli(enqueuedMs)
	return now.Sub(enqueuedAt) > ttl
}
