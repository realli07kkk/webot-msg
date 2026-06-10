package protection

import (
	"context"
	"errors"
	"fmt"
)

const (
	ReasonCount = "count"
	ReasonTime  = "time"
)

type DecisionKind int

const (
	DecisionAllow DecisionKind = iota
	DecisionReject
	DecisionSendReminderAndFreeze
)

type Decision struct {
	Kind   DecisionKind
	Reason string
}

func Allow() Decision {
	return Decision{Kind: DecisionAllow}
}

func Reject(reason string) Decision {
	return Decision{Kind: DecisionReject, Reason: reason}
}

func SendReminderAndFreeze(reason string) Decision {
	return Decision{Kind: DecisionSendReminderAndFreeze, Reason: reason}
}

type Guard interface {
	ReserveNormalSend(ctx context.Context, botID string) (Reservation, error)
	ReleaseNormalSend(ctx context.Context, botID string) error
	RecordReminderSend(ctx context.Context, botID string) error
	RecordActiveConversation(ctx context.Context, botID string) error
	CheckTimeWindow(ctx context.Context, botID string) (Decision, error)
}

type NoopGuard struct{}

type ReservationKind int

const (
	ReservationSendNormal ReservationKind = iota
	ReservationReject
	ReservationSendNormalThenReminder
	ReservationSendReminderOnly
)

type Reservation struct {
	Kind   ReservationKind
	Reason string
}

func SendNormal() Reservation {
	return Reservation{Kind: ReservationSendNormal}
}

func RejectNormal(reason string) Reservation {
	return Reservation{Kind: ReservationReject, Reason: reason}
}

func SendNormalThenReminder(reason string) Reservation {
	return Reservation{Kind: ReservationSendNormalThenReminder, Reason: reason}
}

func SendReminderOnly(reason string) Reservation {
	return Reservation{Kind: ReservationSendReminderOnly, Reason: reason}
}

func (NoopGuard) ReserveNormalSend(context.Context, string) (Reservation, error) {
	return SendNormal(), nil
}

func (NoopGuard) ReleaseNormalSend(context.Context, string) error {
	return nil
}

func (NoopGuard) RecordReminderSend(context.Context, string) error {
	return nil
}

func (NoopGuard) RecordActiveConversation(context.Context, string) error {
	return nil
}

func (NoopGuard) CheckTimeWindow(context.Context, string) (Decision, error) {
	return Allow(), nil
}

type RejectionError struct {
	Reason string
	Err    error
}

func (e *RejectionError) Error() string {
	if e == nil {
		return ""
	}
	msg := "protection mode locked"
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *RejectionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewRejection(reason string, err error) error {
	return &RejectionError{Reason: reason, Err: err}
}

func IsRejection(err error) bool {
	var rejection *RejectionError
	return errors.As(err, &rejection)
}

func RejectionReason(err error) string {
	var rejection *RejectionError
	if errors.As(err, &rejection) {
		return rejection.Reason
	}
	return ""
}

func RejectionMessage(reason string) string {
	if reason == "" {
		return "protection mode locked; send a message from WeChat app before continuing"
	}
	return fmt.Sprintf("protection mode locked (%s); send a message from WeChat app before continuing", reason)
}
