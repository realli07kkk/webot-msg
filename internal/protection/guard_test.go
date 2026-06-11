package protection

import (
	"context"
	"testing"
)

func TestNoopGuardReserveNormalSendHasNoStatusSnapshot(t *testing.T) {
	reservation, err := NoopGuard{}.ReserveNormalSend(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("ReserveNormalSend() error = %v", err)
	}
	if reservation.Kind != ReservationSendNormal {
		t.Fatalf("ReserveNormalSend() = %#v, want send normal", reservation)
	}
	if reservation.HasStatus {
		t.Fatal("Reservation.HasStatus = true, want false")
	}
}
