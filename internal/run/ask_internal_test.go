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

// asking is what the watcher uses to decide whether to ask the queue again.
func TestAskingWaitsAfterARefusalAndForgetsOnReset(t *testing.T) {
	now := time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC)
	const every = 5 * time.Second
	a := &asking{}

	if !a.due(now) {
		t.Error("nothing has refused yet and the watcher is already waiting")
	}

	a.refused(now, every)

	if a.due(now) {
		t.Error("something refused and the watcher asked again at once")
	}
	if !a.due(now.Add(every)) {
		t.Errorf("the watcher is still waiting %s after a refusal", every)
	}
	// Twice as long the second time, so a refusal that stands is asked about
	// less and less.
	a.refused(now, every)
	if a.due(now.Add(every)) {
		t.Error("a second refusal did not wait longer than the first")
	}

	a.reset()

	if !a.due(now) {
		t.Error("the watcher is still waiting after the reason to wait was forgotten")
	}
}
