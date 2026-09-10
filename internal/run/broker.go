package run

import "sync"

// subscriberBuffer is how many lines a follower may fall behind by. The whole
// stream is on disk, so a follower that cannot keep up is dropped and can read
// the log again rather than holding the Agent's output back.
const subscriberBuffer = 1024

// broker fans one Run's output out to whoever is following it, numbering the
// lines so a follower can tell what it has already read from the log file.
type broker struct {
	mu     sync.Mutex
	seq    int64
	next   int
	subs   map[int]chan Line
	closed bool
}

func newBroker() *broker { return &broker{subs: map[int]chan Line{}} }

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
			close(ch)
			delete(b.subs, id)
		}
	}
}

// subscribe returns the lines published from now on, and the way to stop
// listening.
func (b *broker) subscribe() (<-chan Line, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		ch := make(chan Line)
		close(ch)
		return ch, func() {}
	}
	id := b.next
	b.next++
	ch := make(chan Line, subscriberBuffer)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if ch, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(ch)
		}
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

// subscribe follows a Run that is still going. It returns a nil channel for a
// Run that has already ended, whose log is complete on disk.
func (s *Service) subscribe(runID int64) (<-chan Line, func()) {
	s.mu.Lock()
	b := s.brokers[runID]
	s.mu.Unlock()
	if b == nil {
		return nil, nil
	}
	return b.subscribe()
}
