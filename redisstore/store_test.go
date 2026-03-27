package redisstore_test

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	cb "github.com/aqylsoft/circuitbreaker"
	"github.com/aqylsoft/circuitbreaker/redisstore"
	"github.com/redis/go-redis/v9"
)

func setupMiniredis(t *testing.T) (*miniredis.Miniredis, *redisstore.Store) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	store := redisstore.New(client)
	return mr, store
}

func TestStore_GetState_Default(t *testing.T) {
	_, store := setupMiniredis(t)

	state, counts, err := store.GetState("unknown")
	if err != nil {
		t.Fatal(err)
	}

	if state != cb.StateClosed {
		t.Fatalf("expected StateClosed, got %v", state)
	}
	if counts.Requests != 0 {
		t.Fatalf("expected 0 requests, got %d", counts.Requests)
	}
}

func TestStore_SetState_Open(t *testing.T) {
	mr, store := setupMiniredis(t)

	err := store.SetState("test", cb.StateOpen, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	state, _, err := store.GetState("test")
	if err != nil {
		t.Fatal(err)
	}
	if state != cb.StateOpen {
		t.Fatalf("expected StateOpen, got %v", state)
	}

	// Verify TTL was set
	ttl := mr.TTL("cb:test:state")
	if ttl <= 0 {
		t.Fatalf("expected TTL > 0, got %v", ttl)
	}
}

func TestStore_SetState_WithoutTTL(t *testing.T) {
	mr, store := setupMiniredis(t)

	err := store.SetState("test", cb.StateClosed, 0)
	if err != nil {
		t.Fatal(err)
	}

	// No TTL should be set
	ttl := mr.TTL("cb:test:state")
	if ttl != 0 {
		t.Fatalf("expected no TTL (0), got %v", ttl)
	}
}

func TestStore_IncrFailure(t *testing.T) {
	_, store := setupMiniredis(t)

	counts, err := store.IncrFailure("test")
	if err != nil {
		t.Fatal(err)
	}

	if counts.Requests != 1 {
		t.Fatalf("expected 1 request, got %d", counts.Requests)
	}
	if counts.TotalFailures != 1 {
		t.Fatalf("expected 1 failure, got %d", counts.TotalFailures)
	}
	if counts.ConsecutiveFails != 1 {
		t.Fatalf("expected 1 consecutive fail, got %d", counts.ConsecutiveFails)
	}
	if counts.ConsecutiveSucc != 0 {
		t.Fatalf("expected 0 consecutive success, got %d", counts.ConsecutiveSucc)
	}
}

func TestStore_IncrSuccess(t *testing.T) {
	_, store := setupMiniredis(t)

	counts, err := store.IncrSuccess("test")
	if err != nil {
		t.Fatal(err)
	}

	if counts.Requests != 1 {
		t.Fatalf("expected 1 request, got %d", counts.Requests)
	}
	if counts.TotalSuccesses != 1 {
		t.Fatalf("expected 1 success, got %d", counts.TotalSuccesses)
	}
	if counts.ConsecutiveSucc != 1 {
		t.Fatalf("expected 1 consecutive success, got %d", counts.ConsecutiveSucc)
	}
	if counts.ConsecutiveFails != 0 {
		t.Fatalf("expected 0 consecutive fail, got %d", counts.ConsecutiveFails)
	}
}

func TestStore_IncrFailure_ResetsConsecutiveSuccess(t *testing.T) {
	_, store := setupMiniredis(t)

	// Build up some successes
	store.IncrSuccess("test")
	store.IncrSuccess("test")

	// Then fail
	counts, err := store.IncrFailure("test")
	if err != nil {
		t.Fatal(err)
	}

	if counts.ConsecutiveSucc != 0 {
		t.Fatalf("expected 0 consecutive success after failure, got %d", counts.ConsecutiveSucc)
	}
	if counts.ConsecutiveFails != 1 {
		t.Fatalf("expected 1 consecutive fail, got %d", counts.ConsecutiveFails)
	}
}

func TestStore_IncrSuccess_ResetsConsecutiveFailure(t *testing.T) {
	_, store := setupMiniredis(t)

	// Build up some failures
	store.IncrFailure("test")
	store.IncrFailure("test")

	// Then succeed
	counts, err := store.IncrSuccess("test")
	if err != nil {
		t.Fatal(err)
	}

	if counts.ConsecutiveFails != 0 {
		t.Fatalf("expected 0 consecutive fail after success, got %d", counts.ConsecutiveFails)
	}
	if counts.ConsecutiveSucc != 1 {
		t.Fatalf("expected 1 consecutive success, got %d", counts.ConsecutiveSucc)
	}
}

func TestStore_Reset(t *testing.T) {
	_, store := setupMiniredis(t)

	// Set some state
	store.SetState("test", cb.StateOpen, time.Second)
	store.IncrFailure("test")
	store.IncrFailure("test")

	// Reset
	err := store.Reset("test")
	if err != nil {
		t.Fatal(err)
	}

	// Should be back to defaults
	state, counts, err := store.GetState("test")
	if err != nil {
		t.Fatal(err)
	}

	if state != cb.StateClosed {
		t.Fatalf("expected StateClosed after reset, got %v", state)
	}
	if counts.Requests != 0 {
		t.Fatalf("expected 0 requests after reset, got %d", counts.Requests)
	}
}

func TestStore_CustomPrefix(t *testing.T) {
	mr, _ := setupMiniredis(t)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	store := redisstore.New(client, redisstore.WithPrefix("myapp"))

	err := store.SetState("test", cb.StateOpen, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Key should use custom prefix
	if !mr.Exists("myapp:test:state") {
		t.Fatal("expected key with custom prefix 'myapp:test:state'")
	}
}

func TestStore_MultipleBreakers(t *testing.T) {
	_, store := setupMiniredis(t)

	// Different breakers should have independent state
	store.IncrFailure("breaker1")
	store.IncrFailure("breaker1")
	store.IncrSuccess("breaker2")

	_, counts1, _ := store.GetState("breaker1")
	_, counts2, _ := store.GetState("breaker2")

	if counts1.TotalFailures != 2 {
		t.Fatalf("breaker1: expected 2 failures, got %d", counts1.TotalFailures)
	}
	if counts2.TotalSuccesses != 1 {
		t.Fatalf("breaker2: expected 1 success, got %d", counts2.TotalSuccesses)
	}
}
