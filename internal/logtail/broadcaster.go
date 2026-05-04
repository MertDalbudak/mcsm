package logtail

import "sync"

// Subscriber is a buffered channel of LogEntry. The broadcaster uses
// non-blocking sends — slow subscribers will lose entries rather than
// stall every other reader. SubscribeWith lets callers pick the buffer
// depth based on how bursty their consumer is.
type Subscriber chan LogEntry

// Broadcaster fans out LogEntry records to many subscribers. Goroutine-safe.
//
// Used by the log tailer to push parsed lines to:
//   - the in-memory ring (for GET /logs?tail=N)
//   - any number of WS /logs/stream clients
//   - the slot event bus (for player join/leave detection)
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[Subscriber]struct{}
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subscribers: map[Subscriber]struct{}{}}
}

// Subscribe registers a new subscriber and returns it. Pass buf >= 1.
func (b *Broadcaster) Subscribe(buf int) Subscriber {
	if buf < 1 {
		buf = 1
	}
	ch := make(Subscriber, buf)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber and closes its channel. Safe to call
// multiple times; safe even if Publish is concurrently writing — the
// non-blocking send pattern means at worst one entry is dropped.
func (b *Broadcaster) Unsubscribe(ch Subscriber) {
	b.mu.Lock()
	if _, ok := b.subscribers[ch]; ok {
		delete(b.subscribers, ch)
		close(ch)
	}
	b.mu.Unlock()
}

// Publish delivers e to every subscriber. Subscribers whose channel is
// full miss this entry — they're expected to keep up.
func (b *Broadcaster) Publish(e LogEntry) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- e:
		default:
		}
	}
}

// Close terminates all subscribers. Used at shutdown.
func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		close(ch)
	}
	b.subscribers = map[Subscriber]struct{}{}
}

// Count returns the number of active subscribers. Useful for metrics.
func (b *Broadcaster) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
