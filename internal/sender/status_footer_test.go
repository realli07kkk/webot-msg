package sender

import (
	"testing"
	"time"

	"github.com/realli07kkk/webot-msg/internal/protection"
)

func TestProtectionStatusFooter(t *testing.T) {
	tests := []struct {
		name        string
		reservation protection.Reservation
		want        string
	}{
		{
			name: "normal",
			reservation: protection.Reservation{
				HasStatus:              true,
				MessagesBeforeReminder: 4,
				TimeBeforeWarning:      9*time.Hour + 30*time.Minute,
			},
			want: "[限流阈值] 剩余可发 4 条 | 距离限制还有 9h30m",
		},
		{
			name: "zero messages remaining",
			reservation: protection.Reservation{
				HasStatus:              true,
				MessagesBeforeReminder: 0,
				TimeBeforeWarning:      9 * time.Hour,
			},
			want: "[限流阈值] 剩余可发 0 条 | 距离限制还有 9h",
		},
		{
			name: "less than one minute",
			reservation: protection.Reservation{
				HasStatus:              true,
				MessagesBeforeReminder: 1,
				TimeBeforeWarning:      30 * time.Second,
			},
			want: "[限流阈值] 剩余可发 1 条 | 距离限制还有 <1m",
		},
		{
			name:        "no snapshot",
			reservation: protection.SendNormal(),
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := protectionStatusFooter(tt.reservation); got != tt.want {
				t.Fatalf("protectionStatusFooter() = %q, want %q", got, tt.want)
			}
		})
	}
}
