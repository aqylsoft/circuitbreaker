package circuitbreaker

import "sync"

// Registry manages a collection of named circuit breakers.
type Registry struct {
	mu       sync.RWMutex
	breakers map[string]Breaker
}

// NewRegistry creates a new Registry.
func NewRegistry() *Registry {
	return &Registry{
		breakers: make(map[string]Breaker),
	}
}

// Get returns an existing breaker by name, or creates a new one with the
// provided options if it doesn't exist yet.
// Returns an error if the breaker doesn't exist and creation fails due to invalid options.
func (r *Registry) Get(name string, opts ...Option) (Breaker, error) {
	r.mu.RLock()
	if b, ok := r.breakers[name]; ok {
		r.mu.RUnlock()
		return b, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// double-check after acquiring write lock
	if b, ok := r.breakers[name]; ok {
		return b, nil
	}

	b, err := New(name, opts...)
	if err != nil {
		return nil, err
	}
	r.breakers[name] = b
	return b, nil
}

// Breakers returns a snapshot of all registered breakers.
func (r *Registry) Breakers() map[string]Breaker {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot := make(map[string]Breaker, len(r.breakers))
	for k, v := range r.breakers {
		snapshot[k] = v
	}
	return snapshot
}

// Reset resets a specific breaker by name.
func (r *Registry) Reset(name string) {
	r.mu.RLock()
	b, ok := r.breakers[name]
	r.mu.RUnlock()
	if ok {
		b.Reset()
	}
}

// Remove removes a breaker from the registry.
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.breakers, name)
}
