package run

// The broker fans a Run's output out to whoever is following it. A follower
// that cannot keep up is dropped rather than holding the Agent back, which is
// only correct if it is told, so that is what these check.

import "testing"

func TestBrokerFansLinesOutToItsFollowers(t *testing.T) {
	b := newBroker()
	sub := b.subscribe()

	b.publish("one")
	b.publish("two")
	b.close()

	var seen []Line
	for ln := range sub.lines {
		seen = append(seen, ln)
	}
	if len(seen) != 2 || seen[0].Text != "one" || seen[1].Text != "two" {
		t.Fatalf("followed %+v, want both lines in order", seen)
	}
	if seen[0].Seq != 1 || seen[1].Seq != 2 {
		t.Errorf("lines are numbered %d and %d, want 1 and 2", seen[0].Seq, seen[1].Seq)
	}
	if sub.behind() {
		t.Error("a follower that kept up is reported as behind")
	}
}

func TestBrokerDropsAFollowerThatCannotKeepUpAndSaysSo(t *testing.T) {
	b := newBroker()
	sub := b.subscribe()

	for i := 0; i < subscriberBuffer+2; i++ {
		b.publish("line")
	}

	// The channel closes where the follower fell behind, and says why.
	drained := 0
	for range sub.lines {
		drained++
	}
	if drained != subscriberBuffer {
		t.Errorf("the follower received %d lines, want the %d it had room for", drained, subscriberBuffer)
	}
	if !sub.behind() {
		t.Error("a dropped follower is not told it fell behind, so it reads the end of its stream as the end of the run")
	}
}

func TestBrokerSubscribingAfterTheRunEndsGivesAClosedStream(t *testing.T) {
	b := newBroker()
	b.close()

	sub := b.subscribe()

	if _, ok := <-sub.lines; ok {
		t.Error("subscribing to a finished run yielded a line")
	}
	if sub.behind() {
		t.Error("a follower that arrived after the run is reported as behind")
	}
	b.close()
	sub.stop()
}
