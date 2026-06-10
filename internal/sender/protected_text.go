package sender

import (
	"context"
	"fmt"
	"log"

	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/protection"
)

type MessageClient interface {
	SendMessage(user config.UserConfig, to string, text string, contextToken string) error
}

type TextResult struct {
	NormalSent     bool
	ReminderSent   bool
	ReminderReason string
}

func SendProtectedText(ctx context.Context, client MessageClient, guard protection.Guard, user config.UserConfig, text string, reminderText string) (TextResult, error) {
	if guard == nil {
		guard = protection.NoopGuard{}
	}

	result := TextResult{}
	reservation, err := guard.ReserveNormalSend(ctx, user.BotID)
	if err != nil {
		return result, err
	}

	switch reservation.Kind {
	case protection.ReservationReject:
		return result, protection.NewRejection(reservation.Reason, nil)
	case protection.ReservationSendReminderOnly:
		reminderSent, err := SendProtectionReminder(ctx, client, guard, user, reminderText, reservation.Reason)
		result.ReminderSent = reminderSent
		result.ReminderReason = reservation.Reason
		if err != nil {
			return result, err
		}
		return result, protection.NewRejection(reservation.Reason, nil)
	case protection.ReservationSendNormal, protection.ReservationSendNormalThenReminder:
		if err := client.SendMessage(user, user.IlinkUserID, text, user.ContextToken); err != nil {
			if releaseErr := guard.ReleaseNormalSend(ctx, user.BotID); releaseErr != nil {
				return result, fmt.Errorf("send failed: %w; release protection reservation failed: %v", err, releaseErr)
			}
			return result, fmt.Errorf("send failed: %w", err)
		}
		result.NormalSent = true
		if reservation.Kind == protection.ReservationSendNormalThenReminder {
			reminderSent, err := SendProtectionReminder(ctx, client, guard, user, reminderText, reservation.Reason)
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
	if guard == nil {
		guard = protection.NoopGuard{}
	}
	if reminderText == "" || user.IlinkUserID == "" || user.ContextToken == "" {
		return false, nil
	}
	if err := client.SendMessage(user, user.IlinkUserID, reminderText, user.ContextToken); err != nil {
		log.Printf("[Bot: %s] Protection reminder send failed: %v", user.BotID, err)
		return false, nil
	}
	if err := guard.RecordReminderSend(ctx, user.BotID); err != nil {
		return true, fmt.Errorf("record protection reminder failed: %w", err)
	}
	log.Printf("[Bot: %s] Protection reminder sent reason=%s", user.BotID, reason)
	return true, nil
}
