package outbox

import (
	"context"
	"sync"
	"time"
)

// MemoryRow is one row of a MemoryStore.
type MemoryRow struct {
	Key         string
	Retries     int32
	AvailableAt time.Time
	State       string // "pending" or "failed"
	Code        string
	Detail      string
}

// MemoryStore is an in-memory single-slot Store for tests: it honours
// available_at, leases and the same retry/fail semantics as the SQL stores.
type MemoryStore struct {
	mu   sync.Mutex
	now  func() time.Time
	rows map[string]*MemoryRow
	keys []string
}

func NewMemoryStore(now func() time.Time) *MemoryStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryStore{now: now, rows: map[string]*MemoryRow{}}
}

// Enqueue inserts a due pending row, or revives an existing one with a fresh
// retry budget (the behaviour the SQL enqueue statements must have).
func (s *MemoryStore) Enqueue(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[key]; !ok {
		s.keys = append(s.keys, key)
	}
	s.rows[key] = &MemoryRow{Key: key, State: "pending", AvailableAt: s.now()}
}

// Row returns a copy of a row, or false once it was removed.
func (s *MemoryStore) Row(key string) (MemoryRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[key]
	if !ok {
		return MemoryRow{}, false
	}
	return *row, true
}

func (s *MemoryStore) Claim(_ context.Context, limit int32, lease time.Duration) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var claimed []string
	for _, key := range s.keys {
		row, ok := s.rows[key]
		if !ok || row.State != "pending" || row.AvailableAt.After(s.now()) || int32(len(claimed)) >= limit {
			continue
		}
		row.AvailableAt = s.now().Add(lease)
		claimed = append(claimed, key)
	}
	return claimed, nil
}

func (s *MemoryStore) Retries(key string, _ string) int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if row, ok := s.rows[key]; ok {
		return row.Retries
	}
	return 0
}

func (s *MemoryStore) Apply(_ context.Context, key, _ string, resolution Resolution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[key]
	if !ok {
		return nil
	}
	switch resolution.Kind {
	case ResolveDone:
		delete(s.rows, key)
	case ResolveRetry:
		row.Retries++
		row.Code, row.Detail, row.AvailableAt = resolution.Code, resolution.Detail, resolution.At
	case ResolveFail:
		row.State, row.Code, row.Detail = "failed", resolution.Code, resolution.Detail
	case ResolveRelease:
		row.AvailableAt = resolution.At
	}
	return nil
}
