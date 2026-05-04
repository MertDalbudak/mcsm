// Package events implements a typed pub-sub for slot lifecycle and
// gameplay events. The HTTP/WS layer (api/handlers_events.go) is the
// primary subscriber; future code (Discord bots, audit log enrichment)
// will subscribe too.
package events

import (
	"sync"
	"time"
)

// Type tags an Event. Keep in sync with docs/api.md WS frames table.
type Type string

const (
	TypeState        Type = "state"
	TypePlayerJoin   Type = "player_join"
	TypePlayerLeave  Type = "player_leave"
	TypePlayerDeath  Type = "player_death"
	TypePlayerKick   Type = "player_kick"
	TypeChat         Type = "chat"
	TypeTPSSample    Type = "tps_sample"
	TypeError        Type = "error"
)

// Event is a slot-scoped event delivered to subscribers. Fields are
// loose to keep the bus simple; the WS handler renders the documented
// frame shape per type.
type Event struct {
	Type    Type           `json:"type"`
	At      time.Time      `json:"at"`
	From    string         `json:"from,omitempty"`
	To      string         `json:"to,omitempty"`
	Player  string         `json:"player,omitempty"`
	Killer  string         `json:"killer,omitempty"`  // for player_death
	Cause   string         `json:"cause,omitempty"`   // for player_death
	Reason  string         `json:"reason,omitempty"`  // for player_kick
	Message string         `json:"message,omitempty"` // for chat / error
	TPS1m   float64        `json:"tps_1m,omitempty"`
	TPS5m   float64        `json:"tps_5m,omitempty"`
	Code    string         `json:"code,omitempty"`
	Extra   map[string]any `json:"extra,omitempty"`
}

// Bus is one slot's event channel. Subscribers register via Subscribe
// and must call cancel() when done.
type Bus struct {
	mu     sync.RWMutex
	subs   map[int]chan Event
	nextID int
	closed bool
}

const subBuffer = 32

func NewBus() *Bus {
	return &Bus{subs: make(map[int]chan Event)}
}

func (b *Bus) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	id := b.nextID
	b.nextID++
	ch := make(chan Event, subBuffer)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			close(c)
			delete(b.subs, id)
		}
	}
}

func (b *Bus) Publish(e Event) {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, c := range b.subs {
		select {
		case c <- e:
		default:
		}
	}
}

func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, c := range b.subs {
		close(c)
		delete(b.subs, id)
	}
}
