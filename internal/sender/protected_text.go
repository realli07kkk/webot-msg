package sender

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/realli07kkk/webot-msg/internal/audit"
	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/protection"
)

type MessageClient interface {
	SendMessageContext(ctx context.Context, user config.UserConfig, to string, text string, contextToken string) error
}

type TextResult struct {
	NormalSent     bool
	ReminderSent   bool
	ReminderReason string
}

type OutcomeKind int

const (
	OutcomeSent OutcomeKind = iota
	OutcomeQueued
	OutcomeQueueFull
)

type Outcome struct {
	Kind     OutcomeKind
	Result   TextResult
	QueueLen int
	Reason   string
}

const protectionCommitTimeout = 5 * time.Second

type IDGenerator func() (string, error)

type TextOptions struct {
	IDGenerator IDGenerator
	Auditor     audit.Auditor
	Now         func() time.Time
}

func SendProtectedText(ctx context.Context, client MessageClient, guard protection.Guard, user config.UserConfig, text string, reminderText string) (TextResult, error) {
	return SendProtectedTextWithOptions(ctx, client, guard, user, text, reminderText, TextOptions{})
}

func SendProtectedTextWithOptions(ctx context.Context, client MessageClient, guard protection.Guard, user config.UserConfig, text string, reminderText string, opts TextOptions) (TextResult, error) {
	operation := protection.BeginOperation(guard)
	defer operation.Done()

	return sendProtectedText(ctx, client, operation, user, text, reminderText, resolveTextOptions(opts))
}

func SendOrEnqueueText(ctx context.Context, client MessageClient, guard protection.Guard, user config.UserConfig, text string, reminderText string, opts TextOptions) (Outcome, error) {
	operation := protection.BeginOperation(guard)
	defer operation.Done()

	opts = resolveTextOptions(opts)
	if controller, ok := sendQueueController(operation, guard); ok {
		ingress, err := controller.AcquireOrEnqueue(ctx, user.BotID, text)
		if err != nil {
			return Outcome{}, err
		}
		switch ingress.Outcome {
		case protection.IngressSendNow:
			result, err := sendWithReservation(ctx, client, operation, user, text, reminderText, ingress.Reservation, opts)
			if err != nil {
				return Outcome{}, err
			}
			return Outcome{Kind: OutcomeSent, Result: result, Reason: ingress.Reason}, nil
		case protection.IngressQueued:
			result := TextResult{}
			if ingress.SendReminder {
				reminderSent, err := sendProtectionReminder(ctx, client, operation, user, reminderText, ingress.Reason)
				result.ReminderSent = reminderSent
				result.ReminderReason = ingress.Reason
				if err != nil {
					return Outcome{Kind: OutcomeQueued, Result: result, QueueLen: ingress.QueueLen, Reason: ingress.Reason}, err
				}
			}
			log.Printf("[Bot: %s] Message queued by protection mode queue_len=%d reason=%s", user.BotID, ingress.QueueLen, ingress.Reason)
			return Outcome{Kind: OutcomeQueued, Result: result, QueueLen: ingress.QueueLen, Reason: ingress.Reason}, nil
		case protection.IngressQueueFull:
			return Outcome{Kind: OutcomeQueueFull, Reason: ingress.Reason}, nil
		default:
			return Outcome{}, fmt.Errorf("unexpected protection ingress outcome: %d", ingress.Outcome)
		}
	}

	result, err := sendProtectedText(ctx, client, operation, user, text, reminderText, opts)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Kind: OutcomeSent, Result: result}, nil
}

func sendQueueController(operation protection.Operation, guard protection.Guard) (protection.SendQueueController, bool) {
	if controller, ok := operation.(protection.SendQueueController); ok {
		return controller, true
	}
	controller, ok := guard.(protection.SendQueueController)
	return controller, ok
}

func DefaultIDGenerator() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func resolveTextOptions(opts TextOptions) TextOptions {
	if opts.IDGenerator == nil {
		opts.IDGenerator = DefaultIDGenerator
	}
	if opts.Auditor == nil {
		opts.Auditor = audit.NoopAuditor{}
	} else if recorder, ok := opts.Auditor.(*audit.Recorder); ok && recorder == nil {
		opts.Auditor = audit.NoopAuditor{}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return opts
}

func sendProtectedText(ctx context.Context, client MessageClient, guard protection.Guard, user config.UserConfig, text string, reminderText string, opts TextOptions) (TextResult, error) {
	result := TextResult{}
	reservation, err := guard.ReserveNormalSend(ctx, user.BotID)
	if err != nil {
		return result, err
	}

	return sendWithReservation(ctx, client, guard, user, text, reminderText, reservation, opts)
}

func sendWithReservation(ctx context.Context, client MessageClient, guard protection.Guard, user config.UserConfig, text string, reminderText string, reservation protection.Reservation, opts TextOptions) (TextResult, error) {
	result := TextResult{}
	switch reservation.Kind {
	case protection.ReservationReject:
		return result, protection.NewRejection(reservation.Reason, nil)
	case protection.ReservationSendReminderOnly:
		reminderSent, err := sendProtectionReminder(ctx, client, guard, user, reminderText, reservation.Reason)
		result.ReminderSent = reminderSent
		result.ReminderReason = reservation.Reason
		if err != nil {
			return result, err
		}
		return result, protection.NewRejection(reservation.Reason, nil)
	case protection.ReservationSendNormal, protection.ReservationSendNormalThenReminder:
		messageText := text
		if footer := protectionStatusFooter(reservation); footer != "" {
			messageText += "\n" + footer
		}
		messageID, err := opts.IDGenerator()
		if err != nil {
			log.Printf("[Bot: %s] Message ID generation failed; audit skipped: %v", user.BotID, err)
		} else {
			messageText += "\n" + messageID
		}
		if err := client.SendMessageContext(ctx, user, user.IlinkUserID, messageText, user.ContextToken); err != nil {
			commitCtx, cancel := protectionCommitContext()
			releaseErr := guard.ReleaseNormalSend(commitCtx, user.BotID)
			cancel()
			if releaseErr != nil {
				return result, fmt.Errorf("send failed: %w; release protection reservation failed: %v", err, releaseErr)
			}
			return result, fmt.Errorf("send failed: %w", err)
		}
		result.NormalSent = true
		if messageID != "" {
			if err := opts.Auditor.Record(ctx, audit.RecordInput{ID: messageID, SentAt: opts.Now(), Body: messageText}); err != nil {
				log.Printf("[Bot: %s] Message audit record failed: %v", user.BotID, err)
			}
		}
		if reservation.Kind == protection.ReservationSendNormalThenReminder {
			reminderSent, err := sendProtectionReminder(ctx, client, guard, user, reminderText, reservation.Reason)
			result.ReminderSent = reminderSent
			result.ReminderReason = reservation.Reason
			if err != nil {
				return result, err
			}
		}
		return result, nil
	default:
		return result, fmt.Errorf("unexpected protection reservation kind: %d", reservation.Kind)
	}
}

func protectionCommitContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), protectionCommitTimeout)
}

func SendProtectionReminder(ctx context.Context, client MessageClient, guard protection.Guard, user config.UserConfig, reminderText string, reason string) (bool, error) {
	operation := protection.BeginOperation(guard)
	defer operation.Done()

	return sendProtectionReminder(ctx, client, operation, user, reminderText, reason)
}

func sendProtectionReminder(ctx context.Context, client MessageClient, guard protection.Guard, user config.UserConfig, reminderText string, reason string) (bool, error) {
	if reminderText == "" || user.IlinkUserID == "" || user.ContextToken == "" {
		return false, nil
	}
	if err := client.SendMessageContext(ctx, user, user.IlinkUserID, reminderText, user.ContextToken); err != nil {
		log.Printf("[Bot: %s] Protection reminder send failed: %v", user.BotID, err)
		return false, nil
	}
	commitCtx, cancel := protectionCommitContext()
	err := guard.RecordReminderSend(commitCtx, user.BotID)
	cancel()
	if err != nil {
		return true, fmt.Errorf("record protection reminder failed: %w", err)
	}
	log.Printf("[Bot: %s] Protection reminder sent reason=%s", user.BotID, reason)
	return true, nil
}
