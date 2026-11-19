package sse

import (
	"strconv"
	"sync"
	"sync/atomic"
)

// Event represents a Server-Sent Event frame.
type Event struct {
	Name string
	Data string
}

type subscriber struct {
	sessionHash string
	ch          chan Event
	closed      bool
}

// Broker manages thread-safe SSE fan-out and immediate session revocation.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[*subscriber]struct{}
	revision    atomic.Int64
}

// NewBroker constructs a new SSE Broker.
func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[*subscriber]struct{}),
	}
}

// Revision returns the current telemetry change revision.
func (b *Broker) Revision() int64 {
	return b.revision.Load()
}

// Subscribe registers a new client stream bound to sessionHash.
func (b *Broker) Subscribe(sessionHash string) (<-chan Event, func()) {
	sub := &subscriber{
		sessionHash: sessionHash,
		ch:          make(chan Event, 32),
	}
	b.mu.Lock()
	b.subscribers[sub] = struct{}{}
	b.mu.Unlock()

	unsub := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subscribers[sub]; ok {
			delete(b.subscribers, sub)
			if !sub.closed {
				sub.closed = true
				close(sub.ch)
			}
		}
	}
	return sub.ch, unsub
}

// BroadcastChange increments the revision and notifies all active subscribers.
func (b *Broker) BroadcastChange() {
	rev := b.revision.Add(1)
	ev := Event{Name: "change", Data: strconv.FormatInt(rev, 10)}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for sub := range b.subscribers {
		if sub.closed {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
		}
	}
}

// RevokeSession immediately sends an expired event and closes all streams for sessionHash.
func (b *Broker) RevokeSession(sessionHash string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subscribers {
		if sub.sessionHash != sessionHash {
			continue
		}
		delete(b.subscribers, sub)
		if !sub.closed {
			select {
			case sub.ch <- Event{Name: "expired", Data: "{}"}:
			default:
			}
			sub.closed = true
			close(sub.ch)
		}
	}
}

// RevokeAll immediately notifies and closes every connected SSE stream.
func (b *Broker) RevokeAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subscribers {
		delete(b.subscribers, sub)
		if !sub.closed {
			select {
			case sub.ch <- Event{Name: "expired", Data: "{}"}:
			default:
			}
			sub.closed = true
			close(sub.ch)
		}
	}
}

// ActiveSessionHashes returns the distinct session hashes with open SSE streams.
func (b *Broker) ActiveSessionHashes() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	seen := make(map[string]struct{}, len(b.subscribers))
	out := make([]string, 0, len(b.subscribers))
	for sub := range b.subscribers {
		if _, ok := seen[sub.sessionHash]; !ok {
			seen[sub.sessionHash] = struct{}{}
			out = append(out, sub.sessionHash)
		}
	}
	return out
}

// Count returns the number of active subscribers.
func (b *Broker) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Close closes all active SSE subscriber channels during shutdown.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subscribers {
		delete(b.subscribers, sub)
		if !sub.closed {
			sub.closed = true
			close(sub.ch)
		}
	}
}
