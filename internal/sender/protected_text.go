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
			if releaseErr := guard.ReleaseNormalSend(ctx, user.BotID); releaseErr != nil {
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
	if err := guard.RecordReminderSend(ctx, user.BotID); err != nil {
		return true, fmt.Errorf("record protection reminder failed: %w", err)
	}
	log.Printf("[Bot: %s] Protection reminder sent reason=%s", user.BotID, reason)
	return true, nil
}
