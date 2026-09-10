package run

import "sync"

// subscriberBuffer is how many lines a follower may fall behind by. The whole
// stream is on disk, so a follower that cannot keep up is dropped and can read
// the log again rather than holding the Agent's output back.
const subscriberBuffer = 1024

// broker fans one Run's output out to whoever is following it, numbering the
// lines so a follower can tell what it has already read from the log file.
type broker struct {
	mu      sync.Mutex
	seq     int64
	next    int
	subs    map[int]chan Line
	dropped map[int]bool
	closed  bool
}

func newBroker() *broker {
	return &broker{subs: map[int]chan Line{}, dropped: map[int]bool{}}
}

// publish numbers a line and hands it to every follower. A follower that has
// fallen too far behind is dropped rather than blocking the Run.
func (b *broker) publish(text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.seq++
	line := Line{Seq: b.seq, Text: text}
	for id, ch := range b.subs {
		select {
		case ch <- line:
		default:
			// This follower cannot keep up. Its stream ends here, and it is
			// told so rather than being left to read the closed channel as the
			// Run having finished.
			b.dropped[id] = true
			close(ch)
			delete(b.subs, id)
		}
	}
}

// subscription is one follower's view of a Run's output.
type subscription struct {
	lines <-chan Line
	// behind reports, once lines has closed, whether it closed because this
	// follower fell too far behind rather than because the Run ended.
	behind func() bool
	// stop ends the subscription.
	stop func()
}

// subscribe returns the lines published from now on.
func (b *broker) subscribe() subscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		ch := make(chan Line)
		close(ch)
		return subscription{lines: ch, behind: func() bool { return false }, stop: func() {}}
	}
	id := b.next
	b.next++
	ch := make(chan Line, subscriberBuffer)
	b.subs[id] = ch
	return subscription{
		lines: ch,
		behind: func() bool {
			b.mu.Lock()
			defer b.mu.Unlock()
			return b.dropped[id]
		},
		stop: func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.dropped, id)
			if ch, ok := b.subs[id]; ok {
				delete(b.subs, id)
				close(ch)
			}
		},
	}
}

// close ends every follower's stream, which is what tells them the Run is
// over.
func (b *broker) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, ch := range b.subs {
		close(ch)
		delete(b.subs, id)
	}
}

// openBroker starts fanning out a Run's output.
func (s *Service) openBroker(runID int64) *broker {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := newBroker()
	s.brokers[runID] = b
	return b
}

// closeBroker ends the streams of everyone following a Run that is over.
func (s *Service) closeBroker(runID int64) {
	s.mu.Lock()
	b := s.brokers[runID]
	delete(s.brokers, runID)
	s.mu.Unlock()
	if b != nil {
		b.close()
	}
}

// subscribe follows a Run that is still going. ok is false for a Run that has
// already ended, whose log is complete on disk.
func (s *Service) subscribe(runID int64) (subscription, bool) {
	s.mu.Lock()
	b := s.brokers[runID]
	s.mu.Unlock()
	if b == nil {
		return subscription{}, false
	}
	return b.subscribe(), true
}
