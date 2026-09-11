package run

import (
	"testing"
	"time"
)

// nextAsk is what keeps a Job nobody can start from spending the machine this
// product exists to leave alone.
func TestAskingAgainBacksOffAndIsCapped(t *testing.T) {
	const every = 5 * time.Second
	waited := nextAsk(0, every)
	if waited != every {
		t.Errorf("the first wait is %s, want one look", waited)
	}
	// Doubling, so a refusal that stands all night is asked about a handful of
	// times rather than thousands.
	for range 10 {
		grown := nextAsk(waited, every)
		if grown <= waited && grown != maxHold {
			t.Fatalf("waiting %s grew to %s, want it longer or capped", waited, grown)
		}
		waited = grown
	}
	if waited != maxHold {
		t.Errorf("the wait grew to %s, want it capped at %s", waited, maxHold)
	}
	if got := nextAsk(time.Hour, every); got != maxHold {
		t.Errorf("a wait of an hour became %s, want it capped at %s", got, maxHold)
	}
}
