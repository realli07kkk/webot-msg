package sender

import (
	"context"
	"errors"
	"testing"

	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/protection"
)

func TestSendProtectedTextFailsClosedWhenReminderRecordFails(t *testing.T) {
	client := &fakeMessageClient{}
	guard := &fakeGuard{
		reservation:       protection.SendNormalThenReminder(protection.ReasonCount),
		recordReminderErr: protection.NewRejection("redis", errors.New("redis down")),
	}
	user := config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "user-1",
		ContextToken: "ctx-1",
	}

	_, err := SendProtectedText(context.Background(), client, guard, user, "hello", "reminder")
	if err == nil {
		t.Fatal("SendProtectedText() error = nil, want reminder record failure")
	}
	if !protection.IsRejection(err) || protection.RejectionReason(err) != "redis" {
		t.Fatalf("SendProtectedText() error = %v, want redis rejection", err)
	}
	if got := len(client.messages); got != 2 {
		t.Fatalf("sent messages = %#v, want normal + reminder", client.messages)
	}
}

type fakeMessageClient struct {
	messages []string
}

func (f *fakeMessageClient) SendMessage(_ config.UserConfig, _ string, text string, _ string) error {
	f.messages = append(f.messages, text)
	return nil
}

type fakeGuard struct {
	reservation       protection.Reservation
	recordReminderErr error
}

func (f *fakeGuard) ReserveNormalSend(context.Context, string) (protection.Reservation, error) {
	if f.reservation.Kind != protection.ReservationSendNormal {
		return f.reservation, nil
	}
	return protection.SendNormal(), nil
}

func (f *fakeGuard) ReleaseNormalSend(context.Context, string) error {
	return nil
}

func (f *fakeGuard) RecordReminderSend(context.Context, string) error {
	return f.recordReminderErr
}

func (f *fakeGuard) RecordActiveConversation(context.Context, string) error {
	return nil
}

func (f *fakeGuard) CheckTimeWindow(context.Context, string) (protection.Decision, error) {
	return protection.Allow(), nil
}
