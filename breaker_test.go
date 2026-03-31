package circuitbreaker_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cb "github.com/aqylsoft/circuitbreaker"
)

var errFake = errors.New("fake error")

func alwaysFail(_ context.Context) error  { return errFake }
func alwaysSucceed(_ context.Context) error { return nil }

func exec(b cb.Breaker, fn func(context.Context) error) error {
	return b.Execute(context.Background(), func() error { return fn(context.Background()) })
}

func TestConsecutiveFailures_Opens(t *testing.T) {
	b, err := cb.New("test", cb.WithConsecutiveFailures(3), cb.WithOpenTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	for range 3 {
		exec(b, alwaysFail)
	}

	if b.State() != cb.StateOpen {
		t.Fatalf("expected open, got %s", b.State())
	}

	err = exec(b, alwaysSucceed)
	if !errors.Is(err, cb.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestHalfOpen_SuccessCloses(t *testing.T) {
	b, err := cb.New("test",
		cb.WithConsecutiveFailures(2),
		cb.WithOpenTimeout(10*time.Millisecond),
		cb.WithHalfOpenProbes(1),
	)
	if err != nil {
		t.Fatal(err)
	}

	exec(b, alwaysFail)
	exec(b, alwaysFail)

	if b.State() != cb.StateOpen {
		t.Fatal("expected open")
	}

	time.Sleep(20 * time.Millisecond)

	if err := exec(b, alwaysSucceed); err != nil {
		t.Fatalf("probe failed: %v", err)
	}

	if b.State() != cb.StateClosed {
		t.Fatalf("expected closed after successful probe, got %s", b.State())
	}
}

func TestHalfOpen_FailureReopens(t *testing.T) {
	b, err := cb.New("test",
		cb.WithConsecutiveFailures(2),
		cb.WithOpenTimeout(10*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}

	exec(b, alwaysFail)
	exec(b, alwaysFail)
	time.Sleep(20 * time.Millisecond)

	exec(b, alwaysFail) // probe fails

	if b.State() != cb.StateOpen {
		t.Fatalf("expected re-open, got %s", b.State())
	}
}

func TestIsFailure_CustomFilter(t *testing.T) {
	sentinel := errors.New("ignore me")
	b, err := cb.New("test",
		cb.WithConsecutiveFailures(2),
		cb.WithIsFailure(func(err error) bool {
			return err != nil && !errors.Is(err, sentinel)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	// ignored errors should not count
	for range 5 {
		b.Execute(context.Background(), func() error { return sentinel })
	}

	if b.State() != cb.StateClosed {
		t.Fatal("ignored errors should not open breaker")
	}
}

func TestOnStateChange_Callback(t *testing.T) {
	var changes []string
	b, err := cb.New("test",
		cb.WithConsecutiveFailures(2),
		cb.WithOpenTimeout(10*time.Millisecond),
		cb.WithOnStateChange(func(name string, from, to cb.State) {
			changes = append(changes, to.String())
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	exec(b, alwaysFail)
	exec(b, alwaysFail)

	if len(changes) == 0 || changes[len(changes)-1] != "open" {
		t.Fatalf("expected open transition, got %v", changes)
	}
}

func TestReset(t *testing.T) {
	b, err := cb.New("test", cb.WithConsecutiveFailures(2))
	if err != nil {
		t.Fatal(err)
	}
	exec(b, alwaysFail)
	exec(b, alwaysFail)

	b.Reset()

	if b.State() != cb.StateClosed {
		t.Fatal("expected closed after reset")
	}
	if b.Counts().TotalFailures != 0 {
		t.Fatal("expected zero counts after reset")
	}
}

func TestSlidingWindow_Opens(t *testing.T) {
	b, err := cb.New("test",
		cb.WithSlidingWindow(time.Second, 0.5, 5),
		cb.WithOpenTimeout(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}

	// 3 failures out of 4 = 75% > 50%
	exec(b, alwaysFail)
	exec(b, alwaysFail)
	exec(b, alwaysFail)
	exec(b, alwaysSucceed)

	if b.State() != cb.StateOpen {
		t.Fatalf("expected open, got %s", b.State())
	}
}

func TestValidation_InvalidThreshold(t *testing.T) {
	_, err := cb.New("test",
		cb.WithSlidingWindow(time.Second, 1.5, 5), // invalid threshold > 1
		cb.WithOpenTimeout(time.Second),
	)
	if err == nil {
		t.Fatal("expected error for invalid threshold")
	}
}

func TestValidation_InvalidOpenTimeout(t *testing.T) {
	_, err := cb.New("test",
		cb.WithConsecutiveFailures(5),
		cb.WithOpenTimeout(0), // invalid timeout
	)
	if err == nil {
		t.Fatal("expected error for invalid openTimeout")
	}
}

func TestConcurrent_Execute(t *testing.T) {
	b, err := cb.New("test",
		cb.WithConsecutiveFailures(100), // high threshold to avoid opening
		cb.WithOpenTimeout(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var successCount atomic.Int64
	var errorCount atomic.Int64

	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := b.Execute(context.Background(), func() error {
				return nil
			})
			if err != nil {
				errorCount.Add(1)
			} else {
				successCount.Add(1)
			}
		}()
	}

	wg.Wait()

	if successCount.Load() != 100 {
		t.Fatalf("expected 100 successes, got %d (errors: %d)", successCount.Load(), errorCount.Load())
	}
}

func TestConcurrent_HalfOpen_ProbeLimit(t *testing.T) {
	b, err := cb.New("test",
		cb.WithConsecutiveFailures(2),
		cb.WithOpenTimeout(10*time.Millisecond),
		cb.WithHalfOpenProbes(1),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Open the breaker
	exec(b, alwaysFail)
	exec(b, alwaysFail)

	// Wait for half-open
	time.Sleep(20 * time.Millisecond)

	var wg sync.WaitGroup
	var probeCount atomic.Int64
	var tooManyProbesCount atomic.Int64

	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := b.Execute(context.Background(), func() error {
				time.Sleep(5 * time.Millisecond) // simulate work
				return nil
			})
			if errors.Is(err, cb.ErrTooManyProbes) {
				tooManyProbesCount.Add(1)
			} else {
				probeCount.Add(1)
			}
		}()
	}

	wg.Wait()

	// Only 1 probe should have been allowed
	if probeCount.Load() != 1 {
		t.Fatalf("expected 1 probe, got %d", probeCount.Load())
	}
}

func TestOnError_Callback(t *testing.T) {
	var errCalls []string
	b, err := cb.New("test",
		cb.WithConsecutiveFailures(5),
		cb.WithOnError(func(name string, op string, err error) {
			errCalls = append(errCalls, op)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Normal execution should not trigger onError
	exec(b, alwaysSucceed)

	if len(errCalls) != 0 {
		t.Fatalf("expected no error callbacks, got %v", errCalls)
	}
}

// Benchmarks

func BenchmarkExecute_Closed(b *testing.B) {
	breaker, err := cb.New("bench",
		cb.WithConsecutiveFailures(100),
		cb.WithOpenTimeout(time.Second),
	)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	fn := func() error { return nil }

	b.ResetTimer()
	for b.Loop() {
		breaker.Execute(ctx, fn)
	}
}

func BenchmarkExecute_Open(b *testing.B) {
	breaker, err := cb.New("bench",
		cb.WithConsecutiveFailures(1),
		cb.WithOpenTimeout(time.Hour), // keep open
	)
	if err != nil {
		b.Fatal(err)
	}

	// Open the breaker
	breaker.Execute(context.Background(), func() error { return errFake })

	ctx := context.Background()
	fn := func() error { return nil }

	b.ResetTimer()
	for b.Loop() {
		breaker.Execute(ctx, fn)
	}
}

func BenchmarkExecute_Parallel(b *testing.B) {
	breaker, err := cb.New("bench",
		cb.WithConsecutiveFailures(1000000),
		cb.WithOpenTimeout(time.Second),
	)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	fn := func() error { return nil }

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			breaker.Execute(ctx, fn)
		}
	})
}
