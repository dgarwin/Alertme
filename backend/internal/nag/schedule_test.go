package nag

import (
	"testing"
	"time"
)

func TestDelaysWithinSQSLimit(t *testing.T) {
	for i, d := range Delays {
		if d > 900*time.Second {
			t.Errorf("Delays[%d] = %s exceeds SQS max DelaySeconds (900s)", i, d)
		}
	}
}

func TestScheduleCoversExpiryWindow(t *testing.T) {
	var total time.Duration
	for _, d := range Delays {
		total += d
	}
	if total < Expiry {
		t.Errorf("schedule totals %s, shorter than the %s expiry window — pages would go quiet early", total, Expiry)
	}
}

func TestNextDelay(t *testing.T) {
	if d, ok := NextDelay(0); !ok || d != 0 {
		t.Errorf("attempt 0 should fire immediately, got %s ok=%v", d, ok)
	}
	if d, ok := NextDelay(1); !ok || d != 30*time.Second {
		t.Errorf("attempt 1 should wait 30s, got %s ok=%v", d, ok)
	}
	if _, ok := NextDelay(len(Delays) + 1); ok {
		t.Error("schedule should be exhausted past the last delay")
	}
}
