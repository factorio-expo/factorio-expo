package progress

import "sync"

// subscriberBuffer is how many changes a slow stream may lag behind before
// the hub drops changes for it and asks it to re-read instead.
const subscriberBuffer = 32

// Hub fans changes out to the streams open on an entry. A nil *Hub is valid
// and drops everything, so publishers need no wiring in tests.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[*Subscription]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[*Subscription]struct{})}
}

// Subscription receives one entry's changes until Close.
type Subscription struct {
	hub     *Hub
	entryID string
	changes chan Change
	resync  chan struct{}
}

// Changes delivers phase changes and entry-level changes (empty Phase).
func (s *Subscription) Changes() <-chan Change { return s.changes }

// Resync fires when changes were dropped because the stream lagged; the
// stream must re-read the entry.
func (s *Subscription) Resync() <-chan struct{} { return s.resync }

// Close unsubscribes. It is safe to call more than once.
func (s *Subscription) Close() {
	if s.hub == nil {
		return
	}
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	subs := s.hub.subs[s.entryID]
	delete(subs, s)
	if len(subs) == 0 {
		delete(s.hub.subs, s.entryID)
	}
}

// Subscribe starts receiving the entry's changes. Subscribe before reading
// the snapshot, so nothing that happens in between is missed.
func (h *Hub) Subscribe(entryID string) *Subscription {
	sub := &Subscription{
		hub: h, entryID: entryID,
		changes: make(chan Change, subscriberBuffer), resync: make(chan struct{}, 1),
	}
	if h == nil {
		sub.hub = nil
		return sub
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[entryID] == nil {
		h.subs[entryID] = make(map[*Subscription]struct{})
	}
	h.subs[entryID][sub] = struct{}{}
	return sub
}

// Publish delivers a change to the entry's subscribers without blocking.
// Call it after the state it describes is committed.
func (h *Hub) Publish(change Change) {
	if h == nil || change.EntryID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs[change.EntryID] {
		select {
		case sub.changes <- change:
		default:
			select {
			case sub.resync <- struct{}{}:
			default:
			}
		}
	}
}

// EntryChanged tells the entry's streams to re-read it.
func (h *Hub) EntryChanged(entryID string) { h.Publish(Change{EntryID: entryID}) }
