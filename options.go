package circuitbreaker

import (
	"errors"
	"time"
)

const (
	defaultConsecutiveFailures = 5
	defaultOpenTimeout         = 30 * time.Second
	defaultHalfOpenProbes      = 1
	defaultWindowBuckets       = 10
	defaultErrorThreshold      = 0.5
)

// config holds all circuit breaker configuration.
type config struct {
	// Consecutive failures mode
	consecutiveFailures uint32

	// Sliding window mode (used when windowSize > 0)
	windowSize     time.Duration
	windowBuckets  int
	errorThreshold float64

	// Common
	openTimeout    time.Duration
	halfOpenProbes uint32

	// Callbacks
	onStateChange func(name string, from, to State)
	isFailure     func(err error) bool
	onError       func(name string, op string, err error)

	// Backend
	store StateStore
}

func defaultConfig() config {
	return config{
		consecutiveFailures: defaultConsecutiveFailures,
		openTimeout:         defaultOpenTimeout,
		halfOpenProbes:      defaultHalfOpenProbes,
		windowBuckets:       defaultWindowBuckets,
		errorThreshold:      defaultErrorThreshold,
		isFailure:           func(err error) bool { return err != nil },
	}
}

// Option configures a CircuitBreaker.
type Option func(*config)

// WithConsecutiveFailures sets the number of consecutive failures before opening.
func WithConsecutiveFailures(n uint32) Option {
	return func(c *config) { c.consecutiveFailures = n }
}

// WithSlidingWindow enables sliding window mode.
// threshold is the fraction of failures (0.0–1.0) that triggers opening.
// buckets controls time granularity (default 10).
func WithSlidingWindow(windowSize time.Duration, threshold float64, buckets int) Option {
	return func(c *config) {
		c.windowSize = windowSize
		c.errorThreshold = threshold
		if buckets > 0 {
			c.windowBuckets = buckets
		}
	}
}

// WithOpenTimeout sets how long the breaker stays open before probing.
func WithOpenTimeout(d time.Duration) Option {
	return func(c *config) { c.openTimeout = d }
}

// WithHalfOpenProbes sets how many requests are allowed through in half-open state.
func WithHalfOpenProbes(n uint32) Option {
	return func(c *config) { c.halfOpenProbes = n }
}

// WithOnStateChange registers a callback for state transitions.
func WithOnStateChange(fn func(name string, from, to State)) Option {
	return func(c *config) { c.onStateChange = fn }
}

// WithIsFailure overrides which errors count as failures.
// Useful for ignoring context.Canceled, ErrNotFound, etc.
func WithIsFailure(fn func(err error) bool) Option {
	return func(c *config) { c.isFailure = fn }
}

// WithStore sets a custom StateStore backend (e.g. Redis).
func WithStore(s StateStore) Option {
	return func(c *config) { c.store = s }
}

// WithOnError registers a callback for store operation errors.
// Useful for logging or metrics when store operations fail.
func WithOnError(fn func(name string, op string, err error)) Option {
	return func(c *config) { c.onError = fn }
}

// validate checks if the configuration is valid.
func (c *config) validate() error {
	if c.windowSize > 0 {
		if c.errorThreshold < 0 || c.errorThreshold > 1 {
			return errors.New("circuitbreaker: errorThreshold must be between 0.0 and 1.0")
		}
		if c.windowBuckets <= 0 {
			return errors.New("circuitbreaker: windowBuckets must be > 0")
		}
	}
	if c.consecutiveFailures == 0 && c.windowSize == 0 {
		return errors.New("circuitbreaker: either consecutiveFailures or windowSize must be set")
	}
	if c.openTimeout <= 0 {
		return errors.New("circuitbreaker: openTimeout must be > 0")
	}
	if c.halfOpenProbes == 0 {
		return errors.New("circuitbreaker: halfOpenProbes must be > 0")
	}
	return nil
}
