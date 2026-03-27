package circuitbreaker

import (
	"context"
	"sync"
	"time"
)

// State represents the circuit breaker state.
type State int

const (
	StateClosed   State = iota // requests pass through
	StateOpen                  // requests are blocked
	StateHalfOpen              // limited requests pass through as probes
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Breaker is the circuit breaker interface.
type Breaker interface {
	// Execute runs fn through the circuit breaker.
	// Returns ErrCircuitOpen if the breaker is open.
	Execute(ctx context.Context, fn func() error) error

	// State returns the current state.
	State() State

	// Counts returns the current counters.
	Counts() Counts

	// Reset manually resets the breaker to closed state.
	Reset()
}

type breaker struct {
	name   string
	cfg    config
	store  StateStore
	window *slidingWindow // non-nil in sliding window mode

	mu          sync.Mutex
	probeCount  uint32 // active probes in half-open
	openedAt    time.Time
}

// New creates a new Breaker with the given name and options.
// Returns an error if the configuration is invalid.
func New(name string, opts ...Option) (Breaker, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	if cfg.store == nil {
		cfg.store = newMemoryStore()
	}

	b := &breaker{
		name:  name,
		cfg:   cfg,
		store: cfg.store,
	}

	if cfg.windowSize > 0 {
		b.window = newSlidingWindow(cfg.windowSize, cfg.windowBuckets, cfg.errorThreshold)
	}

	return b, nil
}

func (b *breaker) Execute(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := b.beforeCall(); err != nil {
		return err
	}

	err := fn()

	b.afterCall(err)
	return err
}

func (b *breaker) beforeCall() error {
	state, _, err := b.store.GetState(b.name)
	if err != nil {
		b.reportError("GetState", err)
		return err
	}

	switch state {
	case StateOpen:
		// check if timeout elapsed — store auto-expires to half-open
		// re-read after potential auto-transition
		state, _, err = b.store.GetState(b.name)
		if err != nil {
			b.reportError("GetState", err)
		}
		if state == StateOpen {
			return ErrCircuitOpen
		}
		fallthrough

	case StateHalfOpen:
		b.mu.Lock()
		if b.probeCount >= b.cfg.halfOpenProbes {
			b.mu.Unlock()
			return ErrTooManyProbes
		}
		b.probeCount++
		b.mu.Unlock()
	}

	return nil
}

func (b *breaker) afterCall(err error) {
	isFailure := b.cfg.isFailure(err)

	now := time.Now()

	if b.window != nil {
		b.window.record(now, isFailure)
	}

	state, counts, storeErr := b.store.GetState(b.name)
	if storeErr != nil {
		b.reportError("GetState", storeErr)
	}

	if isFailure {
		counts, storeErr = b.store.IncrFailure(b.name)
		if storeErr != nil {
			b.reportError("IncrFailure", storeErr)
		}
		if b.shouldOpen(counts, now) {
			b.transition(state, StateOpen)
		}
	} else {
		counts, storeErr = b.store.IncrSuccess(b.name)
		if storeErr != nil {
			b.reportError("IncrSuccess", storeErr)
		}

		if state == StateHalfOpen {
			b.mu.Lock()
			b.probeCount--
			enough := counts.ConsecutiveSucc >= b.cfg.halfOpenProbes
			b.mu.Unlock()

			if enough {
				b.transition(state, StateClosed)
				if b.window != nil {
					b.window.reset()
				}
				if resetErr := b.store.Reset(b.name); resetErr != nil {
					b.reportError("Reset", resetErr)
				}
			}
		}
	}
}

func (b *breaker) reportError(op string, err error) {
	if b.cfg.onError != nil {
		b.cfg.onError(b.name, op, err)
	}
}

func (b *breaker) shouldOpen(counts Counts, now time.Time) bool {
	if b.window != nil {
		return b.window.shouldOpen(now)
	}
	return counts.ConsecutiveFails >= b.cfg.consecutiveFailures
}

func (b *breaker) transition(from, to State) {
	var ttl time.Duration
	if to == StateOpen {
		ttl = b.cfg.openTimeout
	}
	b.store.SetState(b.name, to, ttl)

	if b.cfg.onStateChange != nil {
		b.cfg.onStateChange(b.name, from, to)
	}
}

func (b *breaker) State() State {
	state, _, _ := b.store.GetState(b.name)
	return state
}

func (b *breaker) Counts() Counts {
	_, counts, _ := b.store.GetState(b.name)
	return counts
}

func (b *breaker) Reset() {
	prev := b.State()
	b.store.Reset(b.name)
	if b.window != nil {
		b.window.reset()
	}
	b.mu.Lock()
	b.probeCount = 0
	b.mu.Unlock()
	if b.cfg.onStateChange != nil && prev != StateClosed {
		b.cfg.onStateChange(b.name, prev, StateClosed)
	}
}
