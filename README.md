# circuitbreaker

A minimal, zero-dependency circuit breaker library for Go.

## Features

- **Two failure models**: consecutive failures or sliding window (error rate %)
- **Context-aware**: `Execute(ctx, fn)` — propagates context cancellation
- **Pluggable state store**: in-memory by default, Redis via `redisstore` subpackage
- **Named breakers**: built-in registry (get-or-create)
- **Custom failure filter**: decide which errors count as failures
- **State change hook**: observe transitions for logging/metrics
- Zero dependencies in core package

## Install

```bash
go get github.com/aqylsoft/circuitbreaker
```

For Redis-backed distributed state:

```bash
go get github.com/aqylsoft/circuitbreaker/redisstore
```

## Quick Start

```go
cb := circuitbreaker.New("stripe",
    circuitbreaker.WithConsecutiveFailures(5),
    circuitbreaker.WithOpenTimeout(30*time.Second),
)

err := cb.Execute(ctx, func() error {
    return stripeClient.Charge(req)
})

if errors.Is(err, circuitbreaker.ErrCircuitOpen) {
    // fast-fail or use fallback
}
```

## Sliding Window Mode

```go
cb := circuitbreaker.New("fraud-api",
    // open if >50% errors in last 30s (10 buckets)
    circuitbreaker.WithSlidingWindow(30*time.Second, 0.5, 10),
    circuitbreaker.WithOpenTimeout(10*time.Second),
)
```

## Custom Failure Filter

```go
cb := circuitbreaker.New("inventory",
    circuitbreaker.WithConsecutiveFailures(3),
    circuitbreaker.WithIsFailure(func(err error) bool {
        // don't count not-found or cancelled requests as failures
        return err != nil &&
            !errors.Is(err, ErrNotFound) &&
            !errors.Is(err, context.Canceled)
    }),
)
```

## State Change Hook

```go
cb := circuitbreaker.New("psp",
    circuitbreaker.WithOnStateChange(func(name string, from, to circuitbreaker.State) {
        log.Printf("breaker %s: %s → %s", name, from, to)
        metrics.BreakerState.WithLabelValues(name).Set(float64(to))
    }),
)
```

## Registry

```go
registry := circuitbreaker.NewRegistry()

// get-or-create by name
cb := registry.Get("stripe", circuitbreaker.WithConsecutiveFailures(5))

// list all
for name, b := range registry.Breakers() {
    fmt.Printf("%s: %s\n", name, b.State())
}
```

## Distributed (Redis)

Use when running multiple instances and you need shared breaker state across pods.

```go
import "github.com/aqylsoft/circuitbreaker/redisstore"

rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
store := redisstore.New(rdb)

cb := circuitbreaker.New("stripe",
    circuitbreaker.WithConsecutiveFailures(5),
    circuitbreaker.WithStore(store), // one line change
)
```

Redis key layout:
```
cb:{name}:state   → integer with TTL (when open)
cb:{name}:counts  → hash with counters
```

### Trade-offs: in-memory vs Redis

| | In-memory | Redis |
|---|---|---|
| Dependencies | none | go-redis |
| Consistency | per-pod | shared across all pods |
| Latency | zero | +network RTT per call |
| Best for | monoliths, few pods | many pods, k8s deployments |

## States

```
CLOSED ──(threshold reached)──► OPEN ──(timeout)──► HALF-OPEN
  ▲                                                       │
  └──────────── probe succeeded ────────────────────────-┘
```

## License

MIT
