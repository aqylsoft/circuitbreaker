package circuitbreaker

import (
	"sync"
	"time"
)

// Counts holds the metrics for a circuit breaker.
type Counts struct {
	Requests         uint32
	TotalFailures    uint32
	TotalSuccesses   uint32
	ConsecutiveFails uint32
	ConsecutiveSucc  uint32
}

// StateStore is a pluggable backend for circuit breaker state and counters.
// The default in-memory implementation is used when no store is provided.
type StateStore interface {
	GetState(name string) (State, Counts, error)
	SetState(name string, state State, ttl time.Duration) error
	IncrSuccess(name string) (Counts, error)
	IncrFailure(name string) (Counts, error)
	Reset(name string) error
}

// memoryStore is the default in-memory StateStore implementation.
type memoryStore struct {
	mu      sync.RWMutex
	entries map[string]*memEntry
}

type memEntry struct {
	state     State
	counts    Counts
	expiresAt time.Time // zero means no expiry
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		entries: make(map[string]*memEntry),
	}
}

func (m *memoryStore) get(name string) *memEntry {
	e, ok := m.entries[name]
	if !ok {
		e = &memEntry{state: StateClosed}
		m.entries[name] = e
	}
	// auto-expire open state
	if !e.expiresAt.IsZero() && time.Now().After(e.expiresAt) {
		e.state = StateHalfOpen
		e.expiresAt = time.Time{}
	}
	return e
}

func (m *memoryStore) GetState(name string) (State, Counts, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.get(name)
	return e.state, e.counts, nil
}

func (m *memoryStore) SetState(name string, state State, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.get(name)
	e.state = state
	if ttl > 0 {
		e.expiresAt = time.Now().Add(ttl)
	} else {
		e.expiresAt = time.Time{}
	}
	return nil
}

func (m *memoryStore) IncrSuccess(name string) (Counts, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.get(name)
	e.counts.Requests++
	e.counts.TotalSuccesses++
	e.counts.ConsecutiveFails = 0
	e.counts.ConsecutiveSucc++
	return e.counts, nil
}

func (m *memoryStore) IncrFailure(name string) (Counts, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.get(name)
	e.counts.Requests++
	e.counts.TotalFailures++
	e.counts.ConsecutiveSucc = 0
	e.counts.ConsecutiveFails++
	return e.counts, nil
}

func (m *memoryStore) Reset(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[name] = &memEntry{state: StateClosed}
	return nil
}
