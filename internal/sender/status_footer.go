package sender

import (
	"fmt"
	"time"

	"github.com/realli07kkk/webot-msg/internal/protection"
)

const protectionStatusFooterPrefix = "[限流阈值]"

func protectionStatusFooter(reservation protection.Reservation) string {
	if !reservation.HasStatus {
		return ""
	}
	return fmt.Sprintf("%s 剩余可发 %d 条 | 距离限制还有 %s",
		protectionStatusFooterPrefix,
		reservation.MessagesBeforeReminder,
		formatStatusFooterDuration(reservation.TimeBeforeWarning),
	)
}

func formatStatusFooterDuration(value time.Duration) string {
	if value < time.Minute {
		return "<1m"
	}
	minutes := int(value / time.Minute)
	hours := minutes / 60
	remainingMinutes := minutes % 60
	if hours > 0 && remainingMinutes > 0 {
		return fmt.Sprintf("%dh%dm", hours, remainingMinutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", remainingMinutes)
}
