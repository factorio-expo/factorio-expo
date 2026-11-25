package httpapi

import "sync"

// Open event-stream caps. A stream holds a connection and a goroutine for up
// to eventsMaxLifetime, so the read rate limit (which counts connects) is not
// enough on its own: a page needs one stream, a few tabs a few more.
const (
	maxStreamsPerClient = 8
	maxStreamsTotal     = 2000
)

// streamSlots counts open event streams per client key (IP, IPv6 /64) and in
// total. A nil *streamSlots allows everything (rate limits off).
type streamSlots struct {
	mu        sync.Mutex
	perClient map[string]int
	total     int
}

func newStreamSlots() *streamSlots {
	return &streamSlots{perClient: make(map[string]int)}
}

// acquire takes a slot for key, or reports false when a cap is reached.
func (s *streamSlots) acquire(key string) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.total >= maxStreamsTotal || s.perClient[key] >= maxStreamsPerClient {
		return false
	}
	s.perClient[key]++
	s.total++
	return true
}

// release returns a slot taken by acquire.
func (s *streamSlots) release(key string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perClient[key]--; s.perClient[key] <= 0 {
		delete(s.perClient, key)
	}
	s.total--
}
