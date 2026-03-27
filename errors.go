package circuitbreaker

import "errors"

// ErrCircuitOpen is returned when the circuit breaker is in the open state.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// ErrTooManyProbes is returned when the half-open probe limit has been reached.
var ErrTooManyProbes = errors.New("circuit breaker: half-open probe limit reached")
